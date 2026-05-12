#!/bin/bash
set -e

# Test script for SOAJS Controller

echo "Running tests..."

# Run unit tests
go test -v -race -cover ./...

# Run with coverage
if [ "$1" == "--coverage" ]; then
    echo "Generating coverage report..."
    go test -v -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report: coverage.html"
fi
