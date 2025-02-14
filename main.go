package main

import (
	"crypto/md5"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/emersion/go-smtp"
)

var smtpaddr = ":1025"
var httpaddr = ":8080"
var certFile = "server.pem"
var keyFile = "server.key"
var baseDir = "/tmp/data"
var filePruneInterval = 15 * time.Minute

var toFilter = ".*"
var fromFilter = ".*"
var toRegexp *regexp.Regexp
var fromRegexp *regexp.Regexp

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}

func init() {
	flag.StringVar(&smtpaddr, "s", getEnv("SMTP_ADDR", smtpaddr), "SMTP listen address")
	flag.StringVar(&httpaddr, "h", getEnv("HTTP_ADDR", httpaddr), "HTTP listen address")
	flag.StringVar(&certFile, "cert", getEnv("TLS_CERT_FILE", certFile), "TLS certificate file")
	flag.StringVar(&keyFile, "key", getEnv("TLS_KEY_FILE", keyFile), "TLS key file")

	flag.StringVar(&toFilter, "toFilter", getEnv("TO_FILTER", toFilter), "To address regexp")
	flag.StringVar(&fromFilter, "fromFilter", getEnv("FROM_FILTER", fromFilter), "From address regexp")

	flag.StringVar(&baseDir, "baseDir", getEnv("BASE_DIR", baseDir), "Base directory to store emails")

}

func SanitizeFilename(to string) string {

	// md5 the "to" string
	m := md5.New()
	m.Write([]byte(to))
	return fmt.Sprintf("%x", m.Sum(nil))

}

type backend struct{}

func (bkd *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &session{}, nil
}

type session struct {
	To         string
	From       string
	ToRegexp   *regexp.Regexp
	FromRegexp *regexp.Regexp
}

func (s *session) AuthPlain(username, password string) error {
	return nil
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {

	if !fromRegexp.MatchString(from) {
		return errors.New("invalid sender")
	}

	s.From = from
	return nil
}

func (s *session) Rcpt(to string, opts *smtp.RcptOptions) error {

	if !toRegexp.MatchString(to) {
		return errors.New("invalid recipient")
	}

	s.To = to
	return nil
}

func (s *session) Data(r io.Reader) error {

	fname := SanitizeFilename(s.To)

	// Create a file to save the mail
	file, err := os.Create(baseDir + "/" + fname)
	if err != nil {
		log.Println("Error creating file", err)
		return err
	}
	defer file.Close()

	_, err = file.WriteString("From: " + s.From + "\n")
	if err != nil {
		log.Println("Error writing From header to file", err)
		return err
	}

	_, err = file.WriteString("To: " + s.To + "\n\n")
	if err != nil {
		log.Println("Error writing To header to file", err)
		return err
	}

	// Write the mail headers and body to the file
	_, err = io.Copy(file, r)
	if err != nil {
		log.Println("Error writing mail body to file", err)
		return err
	}

	return nil
}

func (s *session) Reset() {}

func (s *session) Logout() error {
	return nil
}

func httpHandler(w http.ResponseWriter, r *http.Request) {

	log.Println("HTTP request", r.URL.Path)
	fname := SanitizeFilename(r.PathValue("to"))

	file, err := os.Open(baseDir + "/" + fname)
	if err != nil {
		log.Println("Error opening file", err)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	if err != nil {
		log.Println("Error reading file", err)
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

}

func filePrune(basedir string) {

	for {
		files, err := os.ReadDir(basedir)
		if err != nil {
			log.Println("Error reading directory", err)
			return
		}

		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				log.Println("Error getting file info", err)
				continue
			}
			if time.Since(info.ModTime()) > filePruneInterval {

				err := os.Remove(basedir + "/" + file.Name())
				log.Println("Removing file", file.Name())
				if err != nil {
					log.Println("Error removing file", err)
				}
			}
		}
		// prune stale files every 10 minutes
		time.Sleep(10 * time.Minute)
	}
}

func main() {
	flag.Parse()

	// Compile the regexps to filter to/from email addresses
	toRegexp = regexp.MustCompile(toFilter)
	fromRegexp = regexp.MustCompile(fromFilter)

	// Create the base directory if it doesn't exist
	err := os.MkdirAll(baseDir, 0700)
	if err != nil {
		log.Fatal("Error creating base directory", err)
	}

	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal("Error loading TLS certificate and key:", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	s := smtp.NewServer(&backend{})

	s.Addr = smtpaddr
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	s.TLSConfig = tlsConfig
	s.Debug = os.Stderr

	// Use sane defaults, may want to parameterise this
	s.MaxMessageBytes = 1024 * 1024
	s.WriteTimeout = 60 * time.Second
	s.ReadTimeout = 60 * time.Second
	s.MaxRecipients = 5

	// create a http endpoint
	go func() {
		log.Println("Starting HTTP server at :8080")
		// accept a parameter in the path called "toaddr"
		http.HandleFunc("/mail/{to}", httpHandler)
		log.Fatal(http.ListenAndServe(httpaddr, nil))
	}()

	go func() {
		log.Println("Starting SMTP server at", smtpaddr)
		log.Fatal(s.ListenAndServe())
	}()

	go func() {
		log.Println("Started file pruning service")
		filePrune(baseDir)
	}()

	// wait for all go routines to finish
	select {}

}
