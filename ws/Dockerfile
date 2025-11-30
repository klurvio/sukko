# Stage 1: Build the application
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source tree
# Structure: cmd/, internal/ with organized packages
COPY . .

# Build the server from cmd/server
# -ldflags="-s -w" strips debug information, creating a smaller binary
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /odin-ws ./cmd/server

# Stage 2: Create the final, minimal image
FROM alpine:latest

WORKDIR /root/

# Install ca-certificates for HTTPS connections (Kafka/Redpanda TLS support)
RUN apk --no-cache add ca-certificates

# Copy the static binary from the builder stage
COPY --from=builder /odin-ws .

# Expose the load balancer port (default 3005)
EXPOSE 3005

# Command to run the application
# Flags can be overridden via docker-compose command section:
# --shards: Number of worker shards (default: 3)
# --base-port: Base port for shards (default: 3002)
# --lb-addr: Load balancer address (default: :3005)
# --debug: Enable debug logging
CMD ["./odin-ws", "--shards=3", "--base-port=3002", "--lb-addr=:3005"]
