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
	"strconv"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/emersion/go-smtp"
)

var smtpaddr = ":1025"
var httpaddr = ":8080"
var certFile = "server.pem"
var keyFile = "server.key"
var baseDir = "/tmp/data"
var filePruneInterval = 15 * time.Minute
var maxMessageBytes int64 = 1024 * 1024 // 1MB

var filePruneIntervalVal time.Duration
var filePruneIntervalErr error
var maxMessageBytesVal int64
var maxMessageBytesErr error

var logger *zap.Logger
var logLevel zapcore.Level = zap.ErrorLevel
var logLevelStr string

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

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	valueStr, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}
	if d, err := time.ParseDuration(valueStr); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("invalid value for environment variable %s: %s", key, valueStr)
}// Default log level is Info

func getEnvInt64(key string, fallback int64) (int64, error) {
	valueStr, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}
	if i, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
		return i, nil
	}
	return 0, fmt.Errorf("invalid value for environment variable %s: %s", key, valueStr)
}

func init() {
	flag.StringVar(&smtpaddr, "s", getEnv("SMTP_ADDR", smtpaddr), "SMTP listen address, env $SMTP_ADDR")
	flag.StringVar(&httpaddr, "h", getEnv("HTTP_ADDR", httpaddr), "HTTP listen address, env $HTTP_ADDR")
	flag.StringVar(&certFile, "cert", getEnv("TLS_CERT_FILE", certFile), "TLS certificate file, env $TLS_CERT_FILE")
	flag.StringVar(&keyFile, "key", getEnv("TLS_KEY_FILE", keyFile), "TLS key file, env $TLS_KEY_FILE")

	flag.StringVar(&toFilter, "toFilter", getEnv("TO_FILTER", toFilter), "To address regexp, env $TO_FILTER")
	flag.StringVar(&fromFilter, "fromFilter", getEnv("FROM_FILTER", fromFilter), "From address regexp, env $FROM_FILTER")

	flag.StringVar(&baseDir, "baseDir", getEnv("BASE_DIR", baseDir), "Base directory to store emails, env $BASE_DIR")

	filePruneIntervalVal, filePruneIntervalErr = getEnvDuration("PRUNE_INTERVAL", filePruneInterval)
	flag.DurationVar(&filePruneInterval, "pruneInterval", filePruneIntervalVal, "File prune interval, env $PRUNE_INTERVAL")

	maxMessageBytesVal, maxMessageBytesErr = getEnvInt64("MAX_MESSAGE_BYTES", maxMessageBytes)
	flag.Int64Var(&maxMessageBytes, "maxMessageBytes", maxMessageBytesVal, "Maximum message size in bytes (env $MAX_MESSAGE_BYTES)")

	flag.StringVar(&logLevelStr, "logLevel", getEnv("LOG_LEVEL", "error"), "Logging level, env $LOG_LEVEL (debug, info, warn, error, dpanic, panic, fatal)")

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
		logger.Error("Error creating file", zap.Error(err))
		return err
	}
	defer file.Close()

	_, err = file.WriteString("From: " + s.From + "\n")
	if err != nil {
		logger.Error("Error writing From header to file", zap.Error(err))
		return err
	}

	_, err = file.WriteString("To: " + s.To + "\n\n")
	if err != nil {
		logger.Error("Error writing To header to file", zap.Error(err))
		return err
	}

	// Write the mail headers and body to the file
	_, err = io.Copy(file, r)
	if err != nil {
		logger.Error("Error writing mail body to file", zap.Error(err))
		return err
	}

	return nil
}

func (s *session) Reset() {}

func (s *session) Logout() error {
	return nil
}

func httpHandler(w http.ResponseWriter, r *http.Request) {

	logger.Info("HTTP request", zap.String("path", r.URL.Path))
	fname := SanitizeFilename(r.PathValue("to"))

	file, err := os.Open(baseDir + "/" + fname)
	if err != nil {
		logger.Error("Error opening file", zap.Error(err))
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	if err != nil {
		logger.Error("Error reading file", zap.Error(err))
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

}

func filePrune(basedir string) {

	for {
		files, err := os.ReadDir(basedir)
		if err != nil {
			logger.Error("Error reading directory", zap.Error(err))
			return
		}

		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				logger.Error("Error getting file info", zap.Error(err))
				continue
			}
			if time.Since(info.ModTime()) > filePruneInterval {

				err := os.Remove(basedir + "/" + file.Name())
				logger.Info("Removing file", zap.String("filename", file.Name()))
				if err != nil {
					logger.Error("Error removing file", zap.Error(err))
				}
			}
		}

		time.Sleep(filePruneInterval)
	}
}

func main() {
	flag.Parse()

	var parsedLevel zapcore.Level
	if err := parsedLevel.UnmarshalText([]byte(logLevelStr)); err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	logLevel = parsedLevel

	config := zap.NewProductionConfig()
	config.Level.SetLevel(logLevel)
	var err error
	logger, err = config.Build()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync() // Flushes buffer, if any

	// Replace standard logger with Zap
	zap.ReplaceGlobals(logger.Named("global"))

	// Check for errors from environment variable parsing
	if filePruneIntervalErr != nil {
		logger.Fatal("Error parsing PRUNE_INTERVAL", zap.Error(filePruneIntervalErr))
	}
	if maxMessageBytesErr != nil {
		logger.Fatal("Error parsing MAX_MESSAGE_BYTES", zap.Error(maxMessageBytesErr))
	}

	// Compile the regexps to filter to/from email addresses
	toRegexp = regexp.MustCompile(toFilter)
	fromRegexp = regexp.MustCompile(fromFilter)

	// Create the base directory if it doesn't exist
	err = os.MkdirAll(baseDir, 0700)
	if err != nil {
		logger.Fatal("Error creating base directory", zap.Error(err))
	}

	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		logger.Fatal("Error loading TLS certificate and key", zap.Error(err))
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
	s.MaxMessageBytes = maxMessageBytes
	s.WriteTimeout = 60 * time.Second
	s.ReadTimeout = 60 * time.Second
	s.MaxRecipients = 5

	// create a http endpoint
	go func() {
		logger.Info("Starting HTTP server", zap.String("address", httpaddr))
		// accept a parameter in the path called "toaddr"
		http.HandleFunc("/mail/{to}", httpHandler)
		logger.Fatal("HTTP server failed", zap.Error(http.ListenAndServe(httpaddr, nil)))
	}()

	go func() {
		logger.Info("Starting SMTP server", zap.String("address", smtpaddr))
		logger.Fatal("SMTP server failed", zap.Error(s.ListenAndServe()))
	}()

	go func() {
		logger.Info("Started file pruning service")
		filePrune(baseDir)
	}()

	// wait for all go routines to finish
	select {}

}
