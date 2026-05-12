# SOAJS Controller (Go)

High-performance API Gateway for SOAJS microservices platform, rewritten in Go.

## Features

- Multi-tenant API gateway
- Service discovery and load balancing
- API key authentication
- OAuth 2.0 support
- Rate limiting
- ACL enforcement
- Health monitoring

## Requirements

- Go 1.24+
- MongoDB 5.0+

## Quick Start

### Build

```bash
make build
```

### Run locally

```bash
# Set environment variables
export SOAJS_ENV=dev
export SOAJS_MONGO_URI=mongodb://localhost:27017

# Run
make run
```

### Run with Docker

```bash
# Build and run
cd deployments
docker-compose up -d
```

## Configuration

Configuration can be provided via:
1. Environment variables (SOAJS_ prefix)
2. Config file (configs/default.yaml)

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| SOAJS_ENV | Environment name | dev |
| SOAJS_SERVICE_PORT | HTTP port | 4000 |
| SOAJS_MONGO_URI | MongoDB connection URI | mongodb://localhost:27017 |
| SOAJS_MONGO_DATABASE | Database name | core_provision |
| SOAJS_LOG_LEVEL | Log level (debug/info/warn/error) | info |

## API Endpoints

### Gateway (Port 4000)

All requests are proxied to backend services based on the URL path:
- `/{service}/{version}/{path}` - Route to service

### Maintenance (Port 5000)

| Endpoint | Description |
|----------|-------------|
| GET /heartbeat | Health check |
| GET /loadProvision | Reload provision data |
| GET /reloadRegistry | Reload registry |
| GET /awarenessStat | Get service awareness stats |

## Project Structure

```
├── cmd/controller/      # Application entry point
├── internal/
│   ├── config/         # Configuration
│   ├── server/         # HTTP server
│   ├── middleware/     # Middleware pipeline
│   ├── registry/       # Service registry
│   ├── provision/      # Tenant/key management
│   ├── awareness/      # Service discovery
│   └── lib/            # Internal utilities
├── pkg/
│   ├── core/           # Core utilities
│   ├── mongo/          # MongoDB client
│   ├── types/          # Shared types
│   └── urac/           # User management
├── configs/            # Configuration files
├── deployments/        # Docker/K8s files
└── test/               # Tests
```

## Development

### Run tests

```bash
make test
```

### Run with hot reload

```bash
# Install air first: go install github.com/cosmtrek/air@latest
make watch
```

### Lint

```bash
make lint
```

## License

Apache 2.0 - See LICENSE file
