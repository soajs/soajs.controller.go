# Go Dependencies

## go.mod (Actual)

```go
module github.com/soajs/soajs.controller.go

go 1.26.0

require (
    // HTTP Router
    github.com/go-chi/chi/v5 v5.2.5

    // MongoDB Driver
    go.mongodb.org/mongo-driver v1.17.9

    // Configuration
    github.com/spf13/viper v1.21.0

    // Logging
    go.uber.org/zap v1.28.0

    // UUID Generation
    github.com/google/uuid v1.6.0

    // Password Hashing
    golang.org/x/crypto v0.51.0

    // Rate Limiting
    golang.org/x/time v0.15.0

    // Kubernetes Client (for HA awareness)
    k8s.io/apimachinery v0.36.0
    k8s.io/client-go v0.36.0
)
```

## Node.js to Go Library Mapping

| Node.js Package | Go Package | Version | Notes |
|-----------------|------------|---------|-------|
| `express` / `connect` | `github.com/go-chi/chi/v5` | v5.2.5 | Lightweight router with middleware support |
| `mongodb` | `go.mongodb.org/mongo-driver` | v1.17.9 | Official MongoDB driver |
| `winston` / `bunyan` | `go.uber.org/zap` | v1.28.0 | High-performance structured logging |
| `ajv` | `github.com/santhosh-tekuri/jsonschema/v5` | - | JSON Schema validation (future) |
| `validator` | `github.com/go-playground/validator/v10` | - | Struct validation (future) |
| `uuid` | `github.com/google/uuid` | v1.6.0 | UUID generation |
| `dotenv` | `github.com/spf13/viper` | v1.21.0 | Environment configuration |
| `bcrypt` | `golang.org/x/crypto` | v0.51.0 | Password hashing |
| `async` | Go stdlib (goroutines/channels) | - | Native concurrency |
| `lodash` | Go stdlib | - | Native operations |

## Library Rationale

### HTTP Router: chi v5.2.5

**Why chi over alternatives:**

| Library | Pros | Cons |
|---------|------|------|
| chi | stdlib compatible, lightweight, middleware-focused | Less opinionated |
| gin | Fast, popular | Custom context, heavier |
| echo | Fast, feature-rich | Custom context |
| gorilla/mux | Battle-tested | Maintenance concerns |

**Decision: chi**
- 100% compatible with `net/http` (no vendor lock-in)
- Middleware chain pattern matches SOAJS architecture
- Lightweight (~1000 LOC)
- Active maintenance

### MongoDB: Official Driver v1.17.9

**Why official driver:**
- Direct support from MongoDB Inc.
- Best performance and feature parity
- BSON handling optimized
- Connection pooling built-in
- Transactions support

### Logging: zap v1.28.0

**Why zap:**

| Library | Allocations | Speed | Structured |
|---------|-------------|-------|------------|
| zap | Zero-alloc | Fastest | Yes |
| logrus | Higher | Slower | Yes |
| zerolog | Zero-alloc | Fast | Yes |
| stdlib log | Basic | Fast | No |

**Decision: zap**
- Zero allocation in hot paths
- JSON and console output
- Levels, fields, sampling
- Production-proven at Uber scale

### Configuration: viper v1.21.0

**Why viper:**
- Multiple sources (env, file, remote)
- YAML/JSON/TOML support
- Live watching
- Environment variable binding
- Most popular Go config library

### Kubernetes Client: client-go v0.36.0

**Why k8s client-go:**
- Official Kubernetes client
- Required for HA awareness mode
- Service discovery in K8s environments
- Well-maintained by Kubernetes team

### Rate Limiting: x/time v0.15.0

**Why golang.org/x/time:**
- Token bucket algorithm
- Part of Go extended library
- Well-tested
- Simple API

## Dependency Statistics

| Category | Count |
|----------|-------|
| Direct dependencies | 9 |
| Indirect dependencies | 52 |
| Total packages | 61 |

## Security Considerations

### Dependency Scanning

```bash
# Run vulnerability check
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

### Minimal Dependencies

The Go version has significantly fewer transitive dependencies:

| Metric | Node.js | Go |
|--------|---------|-----|
| Direct deps | ~30 | 9 |
| Transitive deps | ~500+ | ~52 |
| Binary size | N/A | ~38MB |
| Container image | ~200MB | ~20MB |

## Build Constraints

```go
//go:build go1.26
```

Minimum Go version: **1.26** (required by k8s.io/apimachinery)
