# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /controller \
    ./cmd/controller

# Runtime stage
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /controller /app/controller

# Copy config files if they exist
COPY configs/ /app/configs/

# Create non-root user
RUN adduser -D -g '' soajs
USER soajs

# Expose ports (API: 4000, Maintenance: 5000)
EXPOSE 4000 5000

# Health check against maintenance port
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:5000/heartbeat || exit 1

# Run
ENTRYPOINT ["/app/controller"]
