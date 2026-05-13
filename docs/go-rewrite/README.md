# SOAJS Controller Go Rewrite

This documentation covers the feasibility analysis and implementation details for rewriting the SOAJS Controller from Node.js to Go.

## Why Go?

| Aspect | Node.js | Go |
|--------|---------|-----|
| **Performance** | Single-threaded event loop | Native concurrency with goroutines |
| **Memory** | ~150-300MB per instance | ~20-50MB per instance |
| **Deployment** | Requires Node.js runtime | Single static binary |
| **Startup Time** | 2-5 seconds | <100ms |
| **Type Safety** | Runtime errors | Compile-time checks |

## Project Status

The Go rewrite is **complete and functional**, implementing all 17 middleware layers from the Node.js version.

## Documentation

- [Feasibility Assessment](./feasibility-assessment.md) - Risk analysis and effort estimates
- [Architecture Mapping](./architecture-mapping.md) - Node.js to Go component mapping
- [Project Structure](./go-project-structure.md) - Go codebase organization
- [Dependencies](./go-dependencies.md) - Library choices and rationale
- [Implementation Phases](./implementation-phases.md) - Development roadmap

## Quick Start

```bash
# Build
cd soajs.controller.go
go build -o controller ./cmd/controller

# Run
SOAJS_ENV=dashboard \
SOAJS_MONGO_URI=mongodb://localhost:27017 \
./controller
```

## Key Features

- **17-layer middleware pipeline** matching Node.js behavior
- **MongoDB integration** for registry, provision, and OAuth
- **Kubernetes support** for HA mode service discovery
- **Full OAuth2/JWT** token validation
- **ACL merging** from multiple packages
- **Proxy support** to backend services

## Compatibility

The Go controller is designed as a drop-in replacement for the Node.js version, maintaining:

- Same API endpoints (`/soajs/acl`, `/soajs/proxy`)
- Same header conventions (`soajsinjectobj`, `key`, etc.)
- Same MongoDB schema compatibility
- Same environment variable configuration
