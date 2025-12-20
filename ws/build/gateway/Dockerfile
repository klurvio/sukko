# Stage 1: Build the application
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source tree
# Structure: cmd/, internal/ with organized packages
COPY . .

# Build-time version arguments
ARG VERSION=dev
ARG COMMIT_HASH=unknown
ARG BUILD_TIME=unknown

# Build the gateway from cmd/gateway
# -ldflags: -s -w strips debug info, -X injects version variables
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'main.Version=${VERSION}' \
    -X 'main.CommitHash=${COMMIT_HASH}' \
    -X 'main.BuildTime=${BUILD_TIME}'" \
    -o /odin-gateway ./cmd/gateway

# Stage 2: Create the final, minimal image
FROM alpine:latest

WORKDIR /root/

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

# Copy the static binary from the builder stage
COPY --from=builder /odin-gateway .

# Expose the gateway port (default 3000)
EXPOSE 3000

# Command to run the application
# Configuration via environment variables:
# - GATEWAY_PORT: Port to listen on (default: 3000)
# - GATEWAY_BACKEND_URL: ws-server WebSocket URL
# - JWT_SECRET: Secret for JWT validation
# - LOG_LEVEL: debug, info, warn, error
# - LOG_FORMAT: json, text, pretty
CMD ["./odin-gateway"]
