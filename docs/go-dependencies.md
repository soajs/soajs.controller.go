# Go Dependencies

## go.mod

```go
module github.com/soajs/soajs.controller.go

go 1.24

require (
    // HTTP Router
    github.com/go-chi/chi/v5 v5.0.12
    github.com/go-chi/cors v1.2.1

    // MongoDB Driver
    go.mongodb.org/mongo-driver v1.14.0

    // Configuration
    github.com/spf13/viper v1.18.2

    // Logging
    go.uber.org/zap v1.27.0

    // Validation
    github.com/go-playground/validator/v10 v10.18.0
    github.com/santhosh-tekuri/jsonschema/v5 v5.3.1

    // OAuth
    golang.org/x/oauth2 v0.17.0

    // HTTP Client (for proxying)
    github.com/valyala/fasthttp v1.52.0

    // Rate Limiting
    golang.org/x/time v0.5.0

    // Testing
    github.com/stretchr/testify v1.8.4
    github.com/testcontainers/testcontainers-go v0.28.0

    // Utilities
    github.com/google/uuid v1.6.0
    github.com/mitchellh/mapstructure v1.5.0
)
```

## Node.js to Go Library Mapping

| Node.js Package | Go Package | Notes |
|-----------------|------------|-------|
| `express` / `connect` | `github.com/go-chi/chi/v5` | Lightweight router with middleware support |
| `mongodb` | `go.mongodb.org/mongo-driver` | Official MongoDB driver |
| `winston` / `bunyan` | `go.uber.org/zap` | High-performance structured logging |
| `cors` | `github.com/go-chi/cors` | CORS middleware |
| `ajv` | `github.com/santhosh-tekuri/jsonschema/v5` | JSON Schema validation |
| `validator` | `github.com/go-playground/validator/v10` | Struct validation |
| `oauth2-server` | `golang.org/x/oauth2` | OAuth 2.0 client/server |
| `request` / `axios` | `net/http` + `github.com/valyala/fasthttp` | HTTP client for proxying |
| `async` | Go stdlib (goroutines/channels) | Native concurrency |
| `lodash` | Go stdlib | Native operations |
| `uuid` | `github.com/google/uuid` | UUID generation |
| `dotenv` | `github.com/spf13/viper` | Environment configuration |
| `mocha` / `chai` | `testing` + `github.com/stretchr/testify` | Testing framework |

## Library Rationale

### HTTP Router: chi

**Why chi over alternatives:**

| Library | Stars | Pros | Cons |
|---------|-------|------|------|
| chi | 16k+ | stdlib compatible, lightweight, middleware-focused | Less opinionated |
| gin | 73k+ | Fast, popular | Custom context, heavier |
| echo | 27k+ | Fast, feature-rich | Custom context |
| gorilla/mux | 20k+ | Battle-tested | Maintenance concerns |

**Decision: chi**
- 100% compatible with `net/http` (no vendor lock-in)
- Middleware chain pattern matches SOAJS architecture
- Lightweight (~1000 LOC)
- Active maintenance

### MongoDB: Official Driver

**Why official driver:**
- Direct support from MongoDB Inc.
- Best performance and feature parity
- BSON handling optimized
- Connection pooling built-in
- Transactions support

**Alternative considered:**
- `globalsign/mgo` - Deprecated
- `qmgo` - Wrapper, adds overhead

### Logging: zap

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

### Configuration: viper

**Why viper:**
- Multiple sources (env, file, remote)
- YAML/JSON/TOML support
- Live watching
- Environment variable binding
- Most popular Go config library

### Validation: jsonschema + validator

**Dual validation approach:**
- `jsonschema` - For API input validation (matches existing SOAJS schemas)
- `validator` - For internal struct validation

### HTTP Client: fasthttp

**Why fasthttp for proxying:**
- 10x faster than `net/http` for high-throughput proxying
- Lower memory allocations
- Connection pooling optimized for reverse proxy use case

**Note:** Only used for backend proxying, not for the main server.

### Rate Limiting: x/time

**Why golang.org/x/time:**
- Token bucket algorithm
- Part of Go extended library
- Well-tested
- Simple API

### Testing: testify + testcontainers

**testify:**
- Assertions (`assert`, `require`)
- Mocking framework
- Suite support

**testcontainers:**
- Integration testing with real MongoDB
- Docker-based test environments
- Cleanup handled automatically

## Dependency Versions

All dependencies pinned to specific versions for reproducibility:

```go
require (
    // Core - pinned to minor versions
    github.com/go-chi/chi/v5 v5.0.12
    go.mongodb.org/mongo-driver v1.14.0
    go.uber.org/zap v1.27.0

    // Supporting - pinned to patch versions
    github.com/spf13/viper v1.18.2
    github.com/go-playground/validator/v10 v10.18.0
    golang.org/x/oauth2 v0.17.0
    golang.org/x/time v0.5.0
)
```

## Security Considerations

### Dependency Scanning

```yaml
# .github/workflows/security.yml
- name: Run Snyk
  uses: snyk/actions/golang@master

- name: Run govulncheck
  run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

### Minimal Dependencies

The Go version has significantly fewer transitive dependencies:

| Metric | Node.js | Go |
|--------|---------|-----|
| Direct deps | ~30 | ~15 |
| Transitive deps | ~500+ | ~80 |
| Total size | ~200MB | ~20MB |

### Vetted Libraries

All selected libraries:
- Have active maintenance
- Are widely used in production
- Have good security track record
- Pass `go vet` and `staticcheck`

## Build Constraints

```go
//go:build go1.22

package main
```

Minimum Go version: **1.22** (for improved HTTP handling, slices package)
