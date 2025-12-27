# =============================================================================
# Multi-stage build for Go publisher
# =============================================================================

# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o publisher .

# =============================================================================
# Runtime stage
# =============================================================================
FROM alpine:3.19

# Add ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/publisher .

# Expose API port
EXPOSE 3001

# Run the binary
CMD ["./publisher"]
