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

# Build the provisioning service from cmd/provisioning
# -ldflags: -s -w strips debug info, -X injects version variables
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'github.com/Toniq-Labs/odin-ws/internal/shared/version.Version=${VERSION}' \
    -X 'github.com/Toniq-Labs/odin-ws/internal/shared/version.CommitHash=${COMMIT_HASH}' \
    -X 'github.com/Toniq-Labs/odin-ws/internal/shared/version.BuildTime=${BUILD_TIME}'" \
    -o /odin-provisioning ./cmd/provisioning

# Stage 2: Create the final, minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS connections (OIDC JWKS fetch)
RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN addgroup -g 1000 -S odin && \
    adduser -u 1000 -S odin -G odin

WORKDIR /app

# Copy the static binary from the builder stage
COPY --from=builder /odin-provisioning .

# Ensure binary is executable
RUN chmod +x /app/odin-provisioning && chown odin:odin /app/odin-provisioning

# Switch to non-root user
USER odin

# Expose the provisioning API port (default 8080)
EXPOSE 8080

# Command to run the application
# Configuration via environment variables:
# - PROVISIONING_ADDR: Address to listen on (default: :8080)
# - DATABASE_URL: PostgreSQL connection URL (required)
# - AUTH_ENABLED: Enable JWT authentication (default: false)
# - OIDC_ISSUER_URL: OIDC issuer URL (enables OIDC when set with JWKS URL)
# - OIDC_JWKS_URL: OIDC JWKS endpoint URL
# - CORS_ALLOWED_ORIGINS: Comma-separated allowed origins
# - LOG_LEVEL: debug, info, warn, error
# - LOG_FORMAT: json, text, pretty
CMD ["./odin-provisioning"]
