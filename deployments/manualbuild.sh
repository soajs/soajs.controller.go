#!/bin/bash
set -e

# Manual build script for soajs.controller.go
# Builds and pushes Docker image to Docker Hub

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

# Get version from pkg/version/version.json
VERSION=$(jq -r '.version' pkg/version/version.json)
if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
    echo "Error: Could not read version from pkg/version/version.json"
    exit 1
fi

IMAGE_NAME="soajsorg/gateway-go"
MAJOR_VERSION=$(echo "$VERSION" | cut -d. -f1)

echo "========================================"
echo "Building soajs.controller.go"
echo "Version: $VERSION"
echo "Image: $IMAGE_NAME"
echo "========================================"

# Run tests first
echo ""
echo "Running tests..."
go test ./... -v

# Build Go binary locally to verify compilation
echo ""
echo "Verifying Go build..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /tmp/controller-test ./cmd/controller
rm -f /tmp/controller-test
echo "Go build OK"

# Check if logged in to Docker Hub
echo ""
echo "Checking Docker Hub login..."
if ! docker system info 2>/dev/null | grep -q "Username" && ! cat ~/.docker/config.json 2>/dev/null | grep -q "auth"; then
    echo "Not logged in to Docker Hub. Please run: docker login"
    exit 1
fi
echo "Docker login OK"

# Build Docker image for linux/amd64
echo ""
echo "Building Docker image for linux/amd64..."
docker build \
    --platform linux/amd64 \
    -f deployments/Dockerfile \
    -t "$IMAGE_NAME:$VERSION" \
    -t "$IMAGE_NAME:${MAJOR_VERSION}.x" \
    -t "$IMAGE_NAME:latest" \
    .

# Push to Docker Hub
echo ""
echo "Pushing to Docker Hub..."
docker push "$IMAGE_NAME:$VERSION"
docker push "$IMAGE_NAME:${MAJOR_VERSION}.x"
docker push "$IMAGE_NAME:latest"

echo ""
echo "========================================"
echo "Build and push complete!"
echo "Tags pushed:"
echo "  - $IMAGE_NAME:$VERSION"
echo "  - $IMAGE_NAME:${MAJOR_VERSION}.x"
echo "  - $IMAGE_NAME:latest"
echo "========================================"
