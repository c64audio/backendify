# Build stage
FROM golang:1.24.2-alpine AS build

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o backendify ./cmd

# Runtime stage
FROM alpine:3.18

WORKDIR /app

# Install CA certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy the binary from the build stage
COPY --from=build /app/backendify /app/
# Copy configuration file
COPY config.json /app/

# Expose the application port
EXPOSE 9000

# Run as non-root user for security
RUN adduser -D appuser
USER appuser

# Command to run when container starts
ENTRYPOINT ["/app/backendify"]
