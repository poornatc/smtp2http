# Stage 1: Build the Go application
FROM golang:1.23.2 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go module files
COPY go.mod go.sum ./

# Download the Go module dependencies
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the Go application
RUN CGO_ENABLED=0 go build -o smtp2http .

# Stage 2: Create the distroless image
FROM gcr.io/distroless/static-debian12

# Copy the Go application binary from the builder stage
COPY --from=builder /app/smtp2http /smtp2http

# Expose ports 1025 and 8080
EXPOSE 1025 8080

# Command to run the Go application
CMD ["/smtp2http"]
