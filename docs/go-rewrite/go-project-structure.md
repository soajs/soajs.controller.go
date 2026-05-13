# Go Project Structure

## Directory Tree

```
soajs.controller.go/
├── cmd/
│   └── controller/
│       └── main.go              # Application entry point
├── internal/
│   ├── awareness/
│   │   ├── awareness.go         # Awareness interface
│   │   ├── custom.go            # Custom mode (direct MongoDB)
│   │   └── ha.go                # HA mode (Kubernetes)
│   ├── config/
│   │   ├── config.go            # Configuration loading
│   │   ├── defaults.go          # Default values & constants
│   │   └── config_test.go       # Configuration tests
│   ├── lib/
│   │   └── header.go            # Header utilities
│   ├── middleware/
│   │   ├── awareness.go         # Service discovery middleware
│   │   ├── chain.go             # Middleware chaining
│   │   ├── context.go           # SOAJS context initialization
│   │   ├── cors.go              # CORS handling
│   │   ├── enhancer.go          # Request enhancement
│   │   ├── favicon.go           # Favicon serving
│   │   ├── ip2ban.go            # IP blocking
│   │   ├── key.go               # Key validation/decryption
│   │   ├── keyacl.go            # Key-level ACL
│   │   ├── lastseen.go          # Last seen updates
│   │   ├── maintenance.go       # Maintenance mode
│   │   ├── monitor.go           # Request monitoring
│   │   ├── mt.go                # Multi-tenant middleware
│   │   ├── mt_whitelist.go      # MT whitelist handling
│   │   ├── oauth.go             # OAuth/JWT validation
│   │   ├── proxy.go             # Backend proxying
│   │   ├── response.go          # Response formatting
│   │   ├── roaming.go           # OAuth roaming
│   │   ├── soajsroutes.go       # /soajs/* route handlers
│   │   ├── traffic.go           # Traffic control
│   │   ├── url.go               # URL parsing
│   │   └── *_test.go            # Test files
│   ├── provision/
│   │   └── provision.go         # Tenant/key/product management
│   ├── registry/
│   │   ├── cache.go             # Registry caching
│   │   ├── local.go             # Local registry
│   │   ├── mongo.go             # MongoDB registry
│   │   └── registry.go          # Registry interface
│   ├── security/
│   │   └── authorization.go     # Security utilities
│   └── server/
│       ├── controller.go        # Controller setup
│       ├── maintenance.go       # Maintenance server
│       └── server.go            # HTTP server
├── pkg/
│   ├── mongo/
│   │   ├── client.go            # MongoDB client wrapper
│   │   └── options.go           # Connection options
│   ├── security/
│   │   ├── key.go               # Key encryption/decryption
│   │   └── hash.go              # Password hashing
│   ├── types/
│   │   ├── context.go           # Context types
│   │   ├── key.go               # Key types
│   │   ├── product.go           # Product/package types
│   │   ├── registry.go          # Registry types
│   │   ├── service.go           # Service types
│   │   └── tenant.go            # Tenant types
│   └── urac/
│       ├── driver.go            # URAC driver
│       └── types.go             # URAC types
├── test/
│   └── integration/
│       ├── mocks/
│       │   └── services.go      # Mock services
│       ├── gateway_routes_test.go
│       ├── http_methods_test.go
│       ├── maintenance_test.go
│       └── roaming_test.go
├── go.mod                       # Go module definition
├── go.sum                       # Dependency checksums
└── Makefile                     # Build commands
```

## Package Descriptions

### `cmd/controller`
Application entry point. Initializes configuration, creates the controller, and starts the HTTP server.

### `internal/awareness`
Service discovery for locating backend services. Supports two modes:
- **Custom**: Direct MongoDB queries for service hosts
- **HA**: Kubernetes service discovery via DNS

### `internal/config`
Configuration management using Viper. Loads from environment variables and config files. Defines all configuration structs and defaults.

### `internal/middleware`
The 17+ middleware layers that process every request. Each file implements a single middleware concern following the `func(http.Handler) http.Handler` pattern.

### `internal/provision`
Manages tenant, application, key, and product data. Loads from MongoDB and provides lookup methods. Includes OAuth token model.

### `internal/registry`
Registry management for environment configuration. Loads service configurations, custom settings, and resource definitions from MongoDB.

### `internal/server`
HTTP server setup including:
- Main gateway server (port 4000)
- Maintenance server (port 5001)
- Controller orchestration

### `pkg/mongo`
MongoDB client wrapper providing connection pooling and database selection.

### `pkg/security`
Cryptographic utilities:
- Key encryption/decryption (AES)
- Password hashing (bcrypt)

### `pkg/types`
Shared type definitions used across packages. Includes BSON tags for MongoDB mapping.

### `pkg/urac`
URAC (User Registration and Access Control) driver for user lookup and authentication.

## File Counts

| Directory | Go Files | Test Files |
|-----------|----------|------------|
| cmd/ | 1 | 0 |
| internal/awareness/ | 3 | 0 |
| internal/config/ | 2 | 1 |
| internal/lib/ | 1 | 0 |
| internal/middleware/ | 20 | 8 |
| internal/provision/ | 1 | 0 |
| internal/registry/ | 4 | 0 |
| internal/security/ | 1 | 0 |
| internal/server/ | 3 | 0 |
| pkg/mongo/ | 2 | 0 |
| pkg/security/ | 2 | 0 |
| pkg/types/ | 6 | 0 |
| pkg/urac/ | 2 | 0 |
| test/integration/ | 4 | 0 |
| **Total** | **52** | **9** |
