# Go Project Structure

## Complete Directory Tree

```
soajs-controller-go/
├── cmd/
│   └── controller/
│       └── main.go                 # Application entry point
│
├── internal/
│   ├── config/
│   │   ├── config.go               # Configuration loader
│   │   └── defaults.go             # Default configuration values
│   │
│   ├── server/
│   │   ├── server.go               # HTTP server setup
│   │   ├── controller.go           # Controller initialization
│   │   └── maintenance.go          # Maintenance server
│   │
│   ├── middleware/
│   │   ├── chain.go                # Middleware chain builder
│   │   ├── context.go              # SOAJS context middleware
│   │   ├── cors.go                 # CORS handling
│   │   ├── favicon.go              # Favicon handler
│   │   ├── response.go             # Response formatting
│   │   ├── enhancer.go             # Request enrichment
│   │   ├── maintenance.go          # Maintenance mode check
│   │   ├── ip2ban.go               # IP blacklist
│   │   ├── url.go                  # URL parsing
│   │   ├── key.go                  # API key extraction
│   │   ├── keyacl.go               # Key-based ACL
│   │   ├── awareness.go            # Service discovery
│   │   ├── awarenessenv.go         # Env-aware discovery
│   │   ├── proxy.go                # Request proxy (gotoService)
│   │   ├── mt.go                   # Multi-tenant resolution
│   │   ├── oauth.go                # OAuth validation
│   │   ├── traffic.go              # Rate limiting
│   │   └── lastseen.go             # Activity tracking
│   │
│   ├── registry/
│   │   ├── registry.go             # Registry interface
│   │   ├── mongo.go                # MongoDB implementation
│   │   ├── local.go                # Local/file implementation
│   │   └── cache.go                # In-memory caching
│   │
│   ├── provision/
│   │   ├── provision.go            # Provision interface
│   │   ├── tenant.go               # Tenant operations
│   │   ├── key.go                  # Key operations
│   │   ├── product.go              # Product operations
│   │   └── oauth.go                # OAuth model
│   │
│   ├── awareness/
│   │   ├── awareness.go            # Awareness interface
│   │   ├── custom.go               # Custom awareness mode
│   │   ├── ha.go                   # Kubernetes/HA mode
│   │   └── health.go               # Health check logic
│   │
│   └── lib/
│       ├── http.go                 # HTTP utilities
│       ├── ip.go                   # IP detection
│       ├── url.go                  # URL parsing
│       ├── header.go               # Header manipulation
│       ├── request.go              # Request utilities
│       └── apipath.go              # API path regex builder
│
├── pkg/
│   ├── core/
│   │   ├── core.go                 # Core module facade
│   │   ├── logger.go               # Logger wrapper
│   │   ├── security.go             # Security utilities
│   │   ├── key.go                  # Key encryption/decryption
│   │   ├── meta.go                 # Metadata utilities
│   │   ├── validator.go            # JSON schema validation
│   │   └── error.go                # Error handling
│   │
│   ├── mongo/
│   │   ├── client.go               # MongoDB client wrapper
│   │   ├── crud.go                 # CRUD operations
│   │   ├── index.go                # Index management
│   │   └── aggregation.go          # Aggregation helpers
│   │
│   ├── urac/
│   │   ├── driver.go               # URAC driver interface
│   │   ├── user.go                 # User operations
│   │   ├── group.go                # Group operations
│   │   └── model/
│   │       ├── user.go             # User model
│   │       └── group.go            # Group model
│   │
│   └── types/
│       ├── tenant.go               # Tenant types
│       ├── key.go                  # Key types
│       ├── product.go              # Product types
│       ├── service.go              # Service types
│       ├── registry.go             # Registry types
│       └── context.go              # Context types
│
├── api/
│   └── openapi.yaml                # OpenAPI specification
│
├── configs/
│   ├── default.yaml                # Default configuration
│   └── profiles/
│       ├── single.yaml             # Single MongoDB profile
│       ├── replica3.yaml           # Replica set profile
│       └── solo.yaml               # Solo profile
│
├── scripts/
│   ├── build.sh                    # Build script
│   ├── test.sh                     # Test runner
│   └── docker-build.sh             # Docker build script
│
├── deployments/
│   ├── Dockerfile                  # Container image
│   ├── docker-compose.yaml         # Local development
│   └── kubernetes/
│       ├── deployment.yaml         # K8s deployment
│       ├── service.yaml            # K8s service
│       └── configmap.yaml          # K8s configmap
│
├── test/
│   ├── integration/
│   │   ├── gateway_test.go         # Gateway integration tests
│   │   ├── mt_test.go              # Multi-tenant tests
│   │   └── proxy_test.go           # Proxy tests
│   └── fixtures/
│       ├── tenants.json            # Test tenant data
│       ├── products.json           # Test product data
│       └── keys.json               # Test key data
│
├── go.mod                          # Go module definition
├── go.sum                          # Dependency checksums
├── Makefile                        # Build automation
└── README.md                       # Project documentation
```

## Package Purposes

### `cmd/controller/`
Application entry point. Initializes configuration, creates server instance, and starts the HTTP listener.

### `internal/config/`
Configuration management including:
- Environment variable loading
- YAML file parsing
- Configuration validation
- Default value handling

### `internal/server/`
HTTP server lifecycle:
- Server initialization
- Middleware chain setup
- Graceful shutdown
- Maintenance server for health checks

### `internal/middleware/`
All 17 middleware components, each with:
- Configuration options
- Handler implementation
- Unit tests

### `internal/registry/`
Service registry implementation:
- MongoDB-backed persistent storage
- In-memory caching with TTL
- Environment-aware configuration
- Hot-reload support

### `internal/provision/`
Tenant and key management:
- Tenant loading and caching
- API key validation
- Product/package resolution
- OAuth model integration

### `internal/awareness/`
Service discovery:
- Custom mode (direct registration)
- HA mode (Kubernetes service discovery)
- Health checking
- Load balancing

### `internal/lib/`
Shared utilities:
- HTTP helpers
- IP address detection
- URL parsing
- Header manipulation

### `pkg/core/`
Core module (public API):
- Logger factory
- Security utilities
- Key encryption
- Error handling

### `pkg/mongo/`
MongoDB wrapper:
- Connection pooling
- CRUD operations
- Index management
- Aggregation pipelines

### `pkg/urac/`
User access control:
- User management
- Group management
- Permission resolution

### `pkg/types/`
Shared type definitions:
- Data transfer objects
- Configuration structs
- Interface definitions

## File-by-File Breakdown

### Entry Point

**`cmd/controller/main.go`**
```go
package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "github.com/soajs/soajs.controller.go/internal/config"
    "github.com/soajs/soajs.controller.go/internal/server"
)

func main() {
    cfg := config.Load()
    srv := server.New(cfg)

    ctx, cancel := signal.NotifyContext(
        context.Background(),
        syscall.SIGINT,
        syscall.SIGTERM,
    )
    defer cancel()

    srv.Run(ctx)
}
```

### Key Configuration Files

**`internal/config/config.go`**
- Loads configuration from environment and files
- Validates required settings
- Provides typed access to configuration

**`internal/server/controller.go`**
- Initializes all dependencies
- Sets up middleware chain
- Creates HTTP server

**`internal/server/maintenance.go`**
- Maintenance port server
- Health endpoints
- Reload triggers

### Core Middleware Files

**`internal/middleware/context.go`**
- Creates `SOAJSContext` for each request
- Attaches logger, controller context

**`internal/middleware/mt.go`**
- Multi-tenant resolution (largest middleware)
- ACL checking
- OAuth integration point

**`internal/middleware/proxy.go`**
- Request forwarding to backend services
- Header manipulation
- Error handling

### Data Layer Files

**`internal/registry/mongo.go`**
- MongoDB operations for registry
- Service configuration storage

**`internal/provision/tenant.go`**
- Tenant CRUD operations
- Caching layer

**`pkg/mongo/client.go`**
- MongoDB connection management
- Connection pooling
- Reconnection logic
