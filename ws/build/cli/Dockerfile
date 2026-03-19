# Stage 1: Build the CLI binary
FROM golang:1.26.0-alpine AS builder

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

# Build the CLI from cmd/cli
# CGO_ENABLED=0 ensures a static binary without C dependencies
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'github.com/klurvio/sukko/internal/shared/version.Version=${VERSION}' \
    -X 'github.com/klurvio/sukko/internal/shared/version.CommitHash=${COMMIT_HASH}' \
    -X 'github.com/klurvio/sukko/internal/shared/version.BuildTime=${BUILD_TIME}'" \
    -o /sukko ./cmd/cli

# Stage 2: Minimal image with just the binary
FROM alpine:latest

RUN apk --no-cache add ca-certificates

RUN addgroup -S sukko && \
    adduser -u 1000 -S sukko -G sukko

WORKDIR /app

COPY --from=builder /sukko .

RUN chmod +x /app/sukko && chown sukko:sukko /app/sukko

USER sukko

ENTRYPOINT ["./sukko"]
