# SMTP2HTTP

SMTP server to receive emails and serve them over http. smtp2http is intended to be used as a mail server for receiving OTP emails that can be retrieved for [Playwright](https://playwright.dev) automation.


Eg. receive email for `foo@bar.com` and retreive it as http://localhost/mail/`foo@bar.com`.

## Running

```bash
docker run -v $PWD/server.key:/server.key \
    -v $PWD/server.pem:/server.pem \
    -p 25:1025 -p 80:8080 \
    rsubr/smtp2http
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


## TODO

1. Parameterise max email size `MaxMessageBytes`, now hard coded to 1MB.
2. Parameterise the mail prune period `filePruneInterval`, now hard coded to 15 mins.
3. Document the email From/To regex for whitelisting.
4. Document the BASE_DIR for storing emails.