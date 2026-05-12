#!/bin/bash
set -e

# Docker build script for SOAJS Controller

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
IMAGE_NAME=${IMAGE_NAME:-soajs/controller}

echo "Building Docker image ${IMAGE_NAME}:${VERSION}..."

docker build \
    -t ${IMAGE_NAME}:${VERSION} \
    -t ${IMAGE_NAME}:latest \
    -f deployments/Dockerfile \
    .

echo "Docker build complete: ${IMAGE_NAME}:${VERSION}"
