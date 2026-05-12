# Architecture Mapping

## Node.js to Go Module Mapping

### Core Packages

| Node.js Module | Go Package | Notes |
|----------------|------------|-------|
| `soajs.controller` | `github.com/soajs/soajs.controller.go` | Main gateway application |
| `soajs.core.modules` | `github.com/soajs/core` | Core functionality |
| `soajs.urac.driver` | `github.com/soajs/urac` | User management driver |
| `soajs.core.libs` | `github.com/soajs/libs` | Shared utilities |

### Middleware Mapping

| Node.js Middleware | Go Package | Implementation |
|--------------------|------------|----------------|
| `mw/soajs` | `middleware/context` | Request context initialization |
| `mw/cors` | `middleware/cors` | CORS headers handling |
| `mw/favicon` | `middleware/favicon` | Favicon response |
| `mw/response` | `middleware/response` | Response formatting |
| `mw/enhancer` | `middleware/enhancer` | Request enrichment |
| `mw/maintenanceMode` | `middleware/maintenance` | Maintenance mode check |
| `mw/ip2ban` | `middleware/ip2ban` | IP blacklist checking |
| `mw/url` | `middleware/url` | URL parsing/normalization |
| `mw/key` | `middleware/key` | API key extraction |
| `mw/keyACL` | `middleware/keyacl` | Key-based ACL |
| `mw/awareness` | `middleware/awareness` | Service discovery |
| `mw/awarenessEnv` | `middleware/awarenessenv` | Environment-aware discovery |
| `mw/gotoService` | `middleware/proxy` | Request proxying |
| `mw/mt` | `middleware/mt` | Multi-tenant resolution |
| `mw/oauth` | `middleware/oauth` | OAuth validation |
| `mw/traffic` | `middleware/traffic` | Rate limiting |
| `mw/lastSeen` | `middleware/lastseen` | Activity tracking |

## 17-Layer Middleware Pipeline

The SOAJS Controller uses a 17-layer middleware pipeline. Here's the execution order:

```
Request
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│  1. soajs_mw        │ Initialize req.soajs context      │
├─────────────────────────────────────────────────────────┤
│  2. cors_mw         │ Set CORS headers                  │
├─────────────────────────────────────────────────────────┤
│  3. favicon_mw      │ Handle /favicon.ico               │
├─────────────────────────────────────────────────────────┤
│  4. response_mw     │ Attach response helpers           │
├─────────────────────────────────────────────────────────┤
│  5. enhancer_mw     │ Enrich request object             │
├─────────────────────────────────────────────────────────┤
│  6. ip2ban_mw       │ Check IP blacklist                │
├─────────────────────────────────────────────────────────┤
│  7. maintenanceMode │ Check global maintenance          │
├─────────────────────────────────────────────────────────┤
│  8. url_mw          │ Parse and normalize URL           │
├─────────────────────────────────────────────────────────┤
│  9. awareness_mw    │ Service discovery injection       │
├─────────────────────────────────────────────────────────┤
│ 10. awarenessEnv_mw │ Environment-specific awareness    │
├─────────────────────────────────────────────────────────┤
│ 11. key_mw          │ Extract API key from request      │
├─────────────────────────────────────────────────────────┤
│ 12. keyACL_mw       │ Validate key-based permissions    │
├─────────────────────────────────────────────────────────┤
│ 13. gotoService_mw  │ Determine target service          │
├─────────────────────────────────────────────────────────┤
│ 14. oauth_mw*       │ OAuth token validation            │
├─────────────────────────────────────────────────────────┤
│ 15. mt_mw           │ Multi-tenant resolution & ACL     │
├─────────────────────────────────────────────────────────┤
│ 16. traffic_mw      │ Rate limiting & throttling        │
├─────────────────────────────────────────────────────────┤
│ 17. lastSeen_mw     │ Update activity timestamp         │
└─────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│            gotoservice (Proxy to Backend)               │
└─────────────────────────────────────────────────────────┘
    │
    ▼
Response

* oauth_mw is conditionally invoked from within mt_mw
```

## Key Interfaces to Implement

### 1. Context Interface

```go
// SOAJSContext holds request-scoped data
type SOAJSContext struct {
    Log          *Logger
    Controller   *ControllerContext
    Tenant       *TenantInfo
    Key          *KeyInfo
    ServicesConfig map[string]interface{}
    Awareness    *AwarenessClient
    AwarenessEnv *AwarenessEnvClient
    InputMask    interface{}
    Device       *DeviceInfo
    Geo          *GeoInfo
}

// ControllerContext holds gateway-specific data
type ControllerContext struct {
    ServiceParams    *ServiceParams
    Gotoservice      func(http.ResponseWriter, *http.Request)
    RedirectedRequest *http.Request
}
```

### 2. Registry Interface

```go
// Registry manages service configuration and discovery
type Registry interface {
    Load(opts LoadOptions) (*RegistryData, error)
    Get() *RegistryData
    Reload() error
    RegisterHost(host HostInfo) error
    GetHost(serviceName, version string) (*HostInfo, error)
    AddUpdateEnvControllers(data ControllerUpdate) error
}

// RegistryData holds the complete registry state
type RegistryData struct {
    Services       map[string]*ServiceConfig
    Daemons        map[string]*DaemonConfig
    CoreDB         *CoreDBConfig
    ServiceConfig  *GlobalServiceConfig
    CustomRegistry map[string]interface{}
}
```

### 3. Provision Interface

```go
// Provision manages tenant and key data
type Provision interface {
    Init(dbConfig DBConfig) error
    LoadProvision() error
    GetTenant(code string) (*Tenant, error)
    GetKey(key string) (*Key, error)
    GetProduct(code string) (*Product, error)
    GetPackage(productCode, packageCode string) (*Package, error)
    OAuthModel() OAuthModel
}
```

### 4. Middleware Interface

```go
// Middleware is the standard middleware signature
type Middleware func(http.Handler) http.Handler

// MiddlewareConfig holds middleware configuration
type MiddlewareConfig struct {
    Core       *Core
    Log        *Logger
    Provision  Provision
    Registry   Registry
}
```

### 5. Awareness Interface

```go
// Awareness handles service discovery
type Awareness interface {
    GetHost(serviceName string, opts ...HostOption) (*HostInfo, error)
    HealthCheck(serviceName, host string) (*HealthStatus, error)
    RegisterService(info ServiceInfo) error
    DeregisterService(info ServiceInfo) error
}
```

## Data Flow Diagrams

### Request Processing Flow

```
┌──────────┐    ┌─────────────┐    ┌───────────────┐    ┌─────────────┐
│  Client  │───▶│   Gateway   │───▶│  Middleware   │───▶│   Backend   │
│          │    │   Server    │    │   Pipeline    │    │   Service   │
└──────────┘    └─────────────┘    └───────────────┘    └─────────────┘
                      │                    │
                      ▼                    ▼
               ┌─────────────┐    ┌───────────────┐
               │   Registry  │    │   Provision   │
               │   (MongoDB) │    │   (MongoDB)   │
               └─────────────┘    └───────────────┘
```

### Multi-Tenant Resolution Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        mt_mw Flow                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. Extract tenant from key                                     │
│        │                                                        │
│        ▼                                                        │
│  2. Load tenant configuration                                   │
│        │                                                        │
│        ▼                                                        │
│  3. Check maintenance mode (per-tenant)                         │
│        │                                                        │
│        ▼                                                        │
│  4. Resolve product/package ACL                                 │
│        │                                                        │
│        ▼                                                        │
│  5. Check OAuth (if required)  ◀──────  oauth_mw               │
│        │                                                        │
│        ▼                                                        │
│  6. Validate ACL permissions                                    │
│        │                                                        │
│        ▼                                                        │
│  7. Build service parameters                                    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Service Discovery Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Awareness Flow                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐         ┌─────────────┐                       │
│  │   Custom    │   OR    │     HA      │                       │
│  │  Awareness  │         │ (Kubernetes)│                       │
│  └──────┬──────┘         └──────┬──────┘                       │
│         │                       │                               │
│         └───────────┬───────────┘                               │
│                     ▼                                           │
│         ┌─────────────────────┐                                 │
│         │   Health Checker    │                                 │
│         └──────────┬──────────┘                                 │
│                    │                                            │
│         ┌──────────┴──────────┐                                 │
│         ▼                     ▼                                 │
│  ┌─────────────┐       ┌─────────────┐                         │
│  │   Memory    │       │   MongoDB   │                         │
│  │    Cache    │       │   Persist   │                         │
│  └─────────────┘       └─────────────┘                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Pattern Translations

### Callback to Channel/Goroutine

**Node.js Pattern:**
```javascript
provision.loadProvision((loaded) => {
    if (loaded) {
        // continue
    }
});
```

**Go Pattern:**
```go
loaded, err := provision.LoadProvision(ctx)
if err != nil {
    return err
}
// continue
```

### Event Emitter to Channel

**Node.js Pattern:**
```javascript
server.on('error', (err) => {
    log.error(err);
});
```

**Go Pattern:**
```go
go func() {
    for err := range errChan {
        log.Error(err)
    }
}()
```

### Connect Middleware to http.Handler

**Node.js Pattern:**
```javascript
app.use((req, res, next) => {
    // process
    next();
});
```

**Go Pattern:**
```go
func middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // process
        next.ServeHTTP(w, r)
    })
}
```
