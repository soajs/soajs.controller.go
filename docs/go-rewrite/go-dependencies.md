# Go Dependencies

## Direct Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/go-chi/chi/v5` | v5.2.5 | HTTP router and middleware |
| `github.com/google/uuid` | v1.6.0 | UUID generation |
| `github.com/spf13/viper` | v1.21.0 | Configuration management |
| `go.mongodb.org/mongo-driver` | v1.17.9 | MongoDB driver |
| `go.uber.org/zap` | v1.28.0 | Structured logging |
| `golang.org/x/crypto` | v0.51.0 | Cryptographic functions |
| `golang.org/x/time` | v0.15.0 | Rate limiting |
| `k8s.io/apimachinery` | v0.36.0 | Kubernetes types |
| `k8s.io/client-go` | v0.36.0 | Kubernetes client |

## Node.js to Go Library Mapping

| Node.js | Go | Notes |
|---------|-----|-------|
| `express` | `chi` | HTTP routing |
| `mongoose` / `mongodb` | `mongo-driver` | Database access |
| `bcryptjs` | `golang.org/x/crypto/bcrypt` | Password hashing |
| `crypto` (AES) | `crypto/aes`, `crypto/cipher` | Encryption |
| `jsonwebtoken` | Custom implementation | JWT validation |
| `lodash` | Native Go | Utility functions |
| `async` | Goroutines / channels | Async operations |
| `winston` | `zap` | Logging |
| `config` | `viper` | Configuration |
| `kubernetes-client` | `client-go` | K8s integration |

## Why Each Library Was Chosen

### Chi Router (`github.com/go-chi/chi/v5`)
- Lightweight, idiomatic Go
- Middleware-friendly design matching Express patterns
- Built-in middleware (RequestID, RealIP, Recoverer)
- High performance

### MongoDB Driver (`go.mongodb.org/mongo-driver`)
- Official MongoDB Go driver
- Full feature parity with Node.js driver
- Connection pooling built-in
- BSON type support

### Zap Logger (`go.uber.org/zap`)
- High-performance structured logging
- JSON output format
- Level-based filtering
- Field-based context

### Viper (`github.com/spf13/viper`)
- Environment variable binding
- Multiple config file formats
- Nested configuration support
- Hot reloading capability

### Kubernetes Client (`k8s.io/client-go`)
- Official Kubernetes Go client
- Service discovery via DNS
- Pod/endpoint queries
- Used for HA mode awareness

## go.mod

```go
module github.com/soajs/soajs.controller.go

go 1.26.0

require (
    github.com/go-chi/chi/v5 v5.2.5
    github.com/google/uuid v1.6.0
    github.com/spf13/viper v1.21.0
    go.mongodb.org/mongo-driver v1.17.9
    go.uber.org/zap v1.28.0
    golang.org/x/crypto v0.51.0
    golang.org/x/time v0.15.0
    k8s.io/apimachinery v0.36.0
    k8s.io/client-go v0.36.0
)
```

## Indirect Dependencies

The following are pulled in transitively:

- **JSON processing**: `json-iterator/go`, `easyjson`
- **YAML parsing**: `gopkg.in/yaml.v3`
- **Compression**: `klauspost/compress`, `golang/snappy`
- **MongoDB internals**: `xdg-go/scram`, `xdg-go/pbkdf2`
- **Kubernetes internals**: `klog/v2`, `kube-openapi`

## Security Considerations

| Concern | Mitigation |
|---------|------------|
| MongoDB injection | Parameterized queries via driver |
| JWT validation | Signature verification, expiration checks |
| Password storage | bcrypt with configurable cost |
| Key encryption | AES-256 matching Node.js implementation |
| TLS | Standard Go TLS configuration |

## Updating Dependencies

```bash
# Update all dependencies
go get -u ./...

# Update specific dependency
go get -u github.com/go-chi/chi/v5

# Tidy module
go mod tidy
```
