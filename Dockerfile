# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o custom-scheduler .

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /usr/local/bin

# Copy binary from builder
COPY --from=builder /build/custom-scheduler .

# Create non-root user
RUN addgroup -g 1000 scheduler && \
    adduser -D -u 1000 -G scheduler scheduler && \
    chown scheduler:scheduler /usr/local/bin/custom-scheduler

USER scheduler

ENTRYPOINT ["/usr/local/bin/custom-scheduler"]
