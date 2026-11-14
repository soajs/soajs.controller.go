# SOAJS Controller (Go)

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

High-performance API Gateway for SOAJS microservices platform, rewritten in Go.

> **Status**: Core Implementation Complete - Integration Tests Passing (22/22)

## Features

- Multi-tenant API gateway
- Service discovery and load balancing
- API key authentication
- OAuth 2.0 support
- Rate limiting
- ACL enforcement
- Health monitoring
- Kubernetes-native HA mode

## Requirements

- Go 1.26+
- MongoDB 5.0+

## Quick Start

### Build

```bash
go build -o controller ./cmd/controller
```

### Run locally (SOAJS development environment)

```bash
# Option 1: Use the start script
./start_local.sh

# Option 2: Run manually with environment variables
SOAJS_ENV=dashboard SOAJS_PROFILE=/opt/soajs/profile/soajsprofile ./controller
```

The controller will start on:
- **Port 4000**: Main gateway (proxies requests to backend services)
- **Port 5001**: Maintenance endpoints (heartbeat, reload, awareness)

### Run with Docker

```bash
cd deployments
docker-compose up -d
```

### Run unit tests

```bash
make test
```

### Run the conformance tests

The Go gateway must answer as the Node.js gateway does. `test/conformance` replays fixed requests and compares every answer with Node.js's, byte for byte (see test/conformance/README.md):

```bash
test/conformance/run.sh go
```

## Configuration

Configuration can be provided via:
1. Environment variables (SOAJS_ prefix)
2. Config file (configs/default.yaml)

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SOAJS_ENV` | Environment name | dev |
| `SOAJS_SERVICE_PORT` | HTTP port | 4000 |
| `SOAJS_PROFILE` | The profile file: where the provision database lives, the same file the Node.js gateway reads (`module.exports = {"name": "core_provision", "prefix": "", "servers": [...], ...}`) | required |
| `SOAJS_SENSITIVE_ENVS` | JSON array of environments whose tokens are accepted everywhere | `["dashboard"]` |
| `SOAJS_DEPLOY_HA` | Any value: HA mode, no hosts collection | |
| `SOAJS_LOG_LEVEL` | Log level (debug/info/warn/error) | info |

## API Endpoints

### Gateway (Port 4000)

All requests are proxied to backend services based on the URL path:
- `/{service}/{path}` - Route to service (version defaults to 1)
- `/{service}/v{N}/{path}` - Route to specific service version

#### Special Routes

| Route | Description |
|-------|-------------|
| `/soajs/acl` | Get ACL for current tenant/key |
| `/soajs/proxy?proxyRoute=/{service}/{path}` | Proxy to service via query param |

### Maintenance (Port 5001)

| Endpoint | Description |
|----------|-------------|
| `GET /heartbeat` | Health check |
| `GET /loadProvision` | Reload provision data |
| `GET /reloadRegistry` | Reload registry |
| `GET /getRegistry` | Get registry data for a service |
| `GET /awarenessStat` | Get service awareness stats |
| `GET,POST /register` | Register a service host |

## Project Structure

```
├── cmd/controller/      # Application entry point
├── internal/
│   ├── config/         # Configuration management
│   ├── server/         # HTTP server
│   ├── middleware/     # 17-layer middleware pipeline
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
├── docs/               # Documentation
└── test/               # Tests
```

## Middleware Pipeline

The controller implements a 17-layer middleware pipeline:

1. `context` - Initialize SOAJS context
2. `cors` - CORS headers
3. `favicon` - Favicon handler
4. `response` - Response formatting
5. `enhancer` - Request enrichment
6. `ip2ban` - IP blacklist
7. `maintenance` - Maintenance mode check
8. `url` - URL parsing
9. `awareness` - Service discovery
10. `awarenessEnv` - Environment-aware discovery
11. `key` - API key extraction
12. `keyACL` - Key-based permissions
13. `gotoService` - Service routing
14. `oauth` - OAuth validation
15. `mt` - Multi-tenant resolution
16. `traffic` - Rate limiting
17. `lastSeen` - Activity tracking

## Documentation

See the [docs/](./docs/) directory for detailed documentation:

- [Feasibility Assessment](./docs/feasibility-assessment.md)
- [Architecture Mapping](./docs/architecture-mapping.md)
- [Project Structure](./docs/go-project-structure.md)
- [Dependencies](./docs/go-dependencies.md)
- [Implementation Phases](./docs/implementation-phases.md)

## Development

### Prerequisites

```bash
# Install Go 1.26+
# Install MongoDB 5.0+
```

### Build

```bash
make build          # Build binary
make build-linux    # Cross-compile for Linux
make docker         # Build Docker image
```

### Test

```bash
make test           # Run tests
make test-coverage  # Run with coverage report
make lint           # Run linters
```

### Development mode

```bash
make dev            # Run in development mode
make watch          # Run with hot reload (requires air)
```

## Related Projects

- [soajs.controller](https://github.com/soajs/soajs.controller) - Original Node.js implementation
- [soajs.core.modules](https://github.com/soajs/soajs.core.modules) - Core modules
- [soajs.urac.driver](https://github.com/soajs/soajs.urac.driver) - URAC driver

## License

Apache 2.0 - See [LICENSE](LICENSE) file
