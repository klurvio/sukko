# Stage 1: Build the application
FROM golang:1.26.0-alpine AS builder

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

# Build the server from cmd/server
# -ldflags: -s -w strips debug info, -X injects version variables
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'github.com/klurvio/sukko/internal/shared/version.Version=${VERSION}' \
    -X 'github.com/klurvio/sukko/internal/shared/version.CommitHash=${COMMIT_HASH}' \
    -X 'github.com/klurvio/sukko/internal/shared/version.BuildTime=${BUILD_TIME}'" \
    -o /sukko-server ./cmd/server

# Stage 2: Create the final, minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS connections (Kafka/Redpanda TLS support)
RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN 0 && \
    adduser -u 1000 -S sukko -G sukko

WORKDIR /app

# Copy the static binary from the builder stage
COPY --from=builder /sukko-server .

# Ensure binary is executable
RUN chmod +x /app/sukko-server && chown sukko:sukko /app/sukko-server

# Switch to non-root user
USER sukko

# Expose the load balancer port (default 3005)
EXPOSE 3005

# Command to run the application
# Flags can be overridden via docker-compose command section:
# --shards: Number of worker shards (default: 3)
# --base-port: Base port for shards (default: 3002)
# --lb-addr: Load balancer address (default: :3005)
# --debug: Enable debug logging
CMD ["./sukko-server", "--shards=3", "--base-port=3002", "--lb-addr=:3005"]
