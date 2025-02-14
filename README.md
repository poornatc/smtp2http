# SMTP2HTTP

SMTP server to receive emails and serve them over http. smtp2http is intended to be used as a mail server for receiving OTP emails that can be retrieved for [Playwright](https://playwright.dev) automation.


Eg. receive email for `foo@bar.com` and retreive it as http://localhost/mail/`foo@bar.com`.

## Running

```bash
docker run -v $PWD/server.key:/server.key \
    -v $PWD/server.pem:/server.pem \
    smtp2http
```

## Building

```bash
# if using docker buildx
buildx build --load . -t smtp2http

# if using vanilla docker build
docker build . -t smtp2http

```

### Talking to the SMTP server

```bash
#!/bin/bash

netcat -i 1 -C localhost 1025 <<EOT
EHLO localhost
MAIL FROM:<root@nsa.gov>
RCPT TO:<root@gchq.gov.uk>
DATA
Hello world!
.
EOT
```

### Fetching the email over HTTP

```bash
wget -O - -S -q 'http://localhost:8080/mail/root@gchq.gov.uk'
```
