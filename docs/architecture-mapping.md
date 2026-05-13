# Architecture Mapping

## Node.js to Go Component Mapping

### Core Modules

| Node.js | Go Package | Notes |
|---------|------------|-------|
| `mw/cors/index.js` | `internal/middleware/cors.go` | CORS headers |
| `mw/favicon/index.js` | `internal/middleware/favicon.go` | Favicon handler |
| `mw/response/index.js` | `internal/middleware/response.go` | Response formatting |
| `mw/enhancer/index.js` | `internal/middleware/enhancer.go` | Request enhancement |
| `mw/awareness/index.js` | `internal/middleware/awareness.go` | Service discovery |
| `mw/key/index.js` | `internal/middleware/key.go` | Key validation |
| `mw/mt/index.js` | `internal/middleware/mt.go` | Multi-tenant |
| `mw/oauth/index.js` | `internal/middleware/oauth.go` | OAuth validation |
| `mw/traffic/index.js` | `internal/middleware/traffic.go` | Traffic control |

### Supporting Modules

| Node.js | Go Package | Notes |
|---------|------------|-------|
| `soajs.core.modules/core/registry` | `internal/registry/` | Registry management |
| `soajs.core.modules/core/provision` | `internal/provision/` | Provision data |
| `soajs.urac.driver` | `pkg/urac/` | User management |

## Middleware Pipeline (17 Layers)

The Go implementation maintains the exact same middleware order as Node.js:

```
Request
   │
   ▼
┌──────────────────────────────────────┐
│  1. RequestID (chi built-in)         │
│  2. RealIP (chi built-in)            │
│  3. Recoverer (chi built-in)         │
├──────────────────────────────────────┤
│  4. Context - Initialize SOAJS ctx   │
│  5. CORS - Handle CORS headers       │
│  6. Favicon - Serve favicon          │
│  7. Response - Setup response writer │
│  8. Enhancer - Enhance request       │
│  9. IP2Ban - IP blocking check       │
│ 10. MaintenanceMode - Check mode     │
│ 11. URL - Parse URL/service          │
│ 12. Awareness - Service discovery    │
│ 13. AwarenessEnv - Env awareness     │
│ 14. Key - Validate/decrypt key       │
│ 15. KeyACL - Key-level ACL           │
│ 16. GotoService - Route selection    │
│ 17. MT - Multi-tenant resolution     │
│ 18. SoajsRoutesSetup - /soajs setup  │
│ 19. OAuth - Token validation         │
│ 20. UserACL - User-level ACL         │
│ 21. SoajsRoutes - /soajs/* handlers  │
│ 22. Traffic - Traffic control        │
│ 23. LastSeen - Update last seen      │
└──────────────────────────────────────┘
   │
   ▼
 Proxy to Backend Service
```

## Key Interfaces

### Registry Interface

```go
type Registry interface {
    Load(env string) error
    Reload() error
    Get() *types.RegistryData
}
```

### Provision Interface

```go
type Provision interface {
    LoadProvision() error
    SetKeyConfig(algorithm, password string)
    GetTenant(code string) (*types.Tenant, error)
    GetKey(extKey string) (*types.Key, error)
    GetProduct(code string) (*types.Product, error)
    GetPackage(productCode, packageCode string) (*types.Package, error)
    OAuthModel() OAuthModel
}
```

### Awareness Interface

```go
type Awareness interface {
    GetHost(service, version string) (*types.ServiceHost, error)
    RegisterHost(service, version, host string, port int) error
    GetServiceHosts(service string) ([]*types.ServiceHost, error)
}
```

## Data Flow

### Request Processing

```
HTTP Request
     │
     ▼
┌─────────────┐     ┌──────────────┐
│  Chi Router │────▶│  Middleware  │
└─────────────┘     │    Chain     │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
         ┌────────┐   ┌────────┐   ┌────────┐
         │Registry│   │Provision│   │  URAC  │
         └───┬────┘   └────┬───┘   └────┬───┘
             │             │            │
             └─────────────┼────────────┘
                           ▼
                    ┌─────────────┐
                    │   MongoDB   │
                    └─────────────┘
```

### ACL Resolution Flow

```
1. Extract key from request header
2. Decrypt external key → internal key
3. Look up tenant/application/package
4. Get user's allowed packages from OAuth token
5. Fetch ACL for each package
6. Merge all ACLs (union operation)
7. Apply to request context
```

## BSON Type Handling

Go requires explicit BSON type conversion:

| BSON Type | Go Type | Conversion |
|-----------|---------|------------|
| `primitive.ObjectID` | `string` | `.Hex()` |
| `bson.D` | `map[string]interface{}` | Iterate elements |
| `bson.M` | `map[string]interface{}` | Direct cast |
| `bson.A` | `[]interface{}` | Direct cast |

The `convertBSONMap()` and `convertBSONValue()` functions handle recursive conversion.
