#!/bin/bash
set -e

# Build script for SOAJS Controller

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

echo "Building SOAJS Controller ${VERSION}..."

go build -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
    -o bin/controller \
    ./cmd/controller

echo "Build complete: bin/controller"
