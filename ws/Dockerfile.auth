# Stage 1: Build the application
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source tree
COPY . .

# Build-time version arguments
ARG VERSION=dev
ARG COMMIT_HASH=unknown
ARG BUILD_TIME=unknown

# Build the auth service from cmd/auth
# -ldflags: -s -w strips debug info, -X injects version variables
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'github.com/adred-codev/odin-ws/internal/version.Version=${VERSION}' \
    -X 'github.com/adred-codev/odin-ws/internal/version.CommitHash=${COMMIT_HASH}' \
    -X 'github.com/adred-codev/odin-ws/internal/version.BuildTime=${BUILD_TIME}'" \
    -o /odin-auth ./cmd/auth

# Stage 2: Create the final, minimal image
FROM alpine:latest

WORKDIR /root/

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

# Copy the static binary from the builder stage
COPY --from=builder /odin-auth .

# Expose the auth service port (default 3002)
EXPOSE 3002

# Command to run the application
CMD ["./odin-auth"]
