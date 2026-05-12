.PHONY: all build clean test lint run docker

# Build variables
BINARY_NAME=controller
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go variables
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

all: lint test build

build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/controller

build-linux:
	@echo "Building $(BINARY_NAME) for Linux..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/controller

clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf bin/

test:
	@echo "Running tests..."
	$(GOTEST) -v -race -cover ./...

test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

run: build
	@echo "Running $(BINARY_NAME)..."
	./bin/$(BINARY_NAME)

docker:
	@echo "Building Docker image..."
	docker build -t soajs/controller:$(VERSION) .

docker-push:
	@echo "Pushing Docker image..."
	docker push soajs/controller:$(VERSION)

deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download

tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

vendor:
	@echo "Vendoring dependencies..."
	$(GOMOD) vendor

fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

vet:
	@echo "Vetting code..."
	$(GOCMD) vet ./...

# Development helpers
dev: build
	@echo "Starting in development mode..."
	SOAJS_ENV=dev ./bin/$(BINARY_NAME)

watch:
	@echo "Watching for changes..."
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "air not installed. Install with: go install github.com/cosmtrek/air@latest"; \
	fi
