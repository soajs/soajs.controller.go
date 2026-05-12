# Implementation Phases

## Overview

The Go rewrite is divided into three phases, with clear dependencies and milestones.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Implementation Timeline                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Week 1    Week 2    Week 3    Week 4    Week 5    Week 6-9    │
│  ────────────────    ────────────────    ────────────────────  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │   Phase 1    │───▶│   Phase 2    │───▶│     Phase 3      │  │
│  │    Core      │    │    URAC      │    │    Gateway MW    │  │
│  │ Infrastructure│    │   Driver     │    │                  │  │
│  └──────────────┘    └──────────────┘    └──────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Core Infrastructure (Weeks 1-3)

### Objective
Build the foundation: HTTP server, configuration, MongoDB integration, and basic middleware.

### Deliverables

| Component | Description | Est. Days |
|-----------|-------------|-----------|
| Project scaffold | Directory structure, go.mod, Makefile | 0.5 |
| Configuration system | Viper-based config loading | 1 |
| Logger setup | Zap logger with SOAJS format | 0.5 |
| HTTP server | Chi router, graceful shutdown | 1 |
| MongoDB client | Connection pool, CRUD wrapper | 2 |
| Registry module | Service discovery, caching | 3 |
| Basic middleware | context, cors, favicon, response | 2 |
| Maintenance server | Health endpoints, reload | 1 |
| Unit tests | Core component tests | 2 |
| **Total** | | **13 days** |

### Detailed Tasks

#### Week 1: Foundation

**Day 1-2: Project Setup**
```
- Initialize go.mod
- Create directory structure
- Set up Makefile with common targets
- Configure linting (golangci-lint)
- Set up CI pipeline skeleton
```

**Day 3-4: Configuration & Logging**
```
- Implement config.Load() with Viper
- Map environment variables to config struct
- Support YAML config files
- Initialize Zap logger with SOAJS format
- Add log levels and structured fields
```

**Day 5: HTTP Server**
```
- Create Chi router
- Implement graceful shutdown
- Add request ID middleware
- Set up server options (timeouts, limits)
```

#### Week 2: Data Layer

**Day 6-7: MongoDB Client**
```
- Implement connection manager
- Create CRUD wrapper methods
- Add connection pooling
- Implement retry logic
- Add index management helpers
```

**Day 8-10: Registry Module**
```
- Define Registry interface
- Implement MongoDB storage
- Add in-memory cache layer
- Implement hot-reload
- Add environment awareness
```

#### Week 3: Basic Middleware

**Day 11-12: Core Middleware**
```
- context.go - SOAJSContext initialization
- cors.go - CORS headers
- favicon.go - Favicon handler
- response.go - Response formatting helpers
```

**Day 13: Maintenance Server**
```
- Separate maintenance port listener
- /heartbeat endpoint
- /loadProvision endpoint
- /reloadRegistry endpoint
```

**Day 14-15: Testing**
```
- Unit tests for all components
- Integration test with testcontainers
- CI pipeline completion
```

### Phase 1 Exit Criteria

- [ ] Server starts and responds to /heartbeat
- [ ] MongoDB connection established
- [ ] Registry loads from database
- [ ] Basic middleware chain executes
- [ ] All unit tests pass
- [ ] CI pipeline green

---

## Phase 2: URAC Driver (Weeks 3-4)

### Objective
Implement user authentication and access control driver.

### Deliverables

| Component | Description | Est. Days |
|-----------|-------------|-----------|
| URAC driver interface | Define contracts | 0.5 |
| User model | User struct and operations | 1.5 |
| Group model | Group struct and operations | 1 |
| User service | User lookup, validation | 2 |
| Group service | Group membership | 1 |
| Permission resolver | ACL calculation | 2 |
| Integration tests | End-to-end auth tests | 2 |
| **Total** | | **10 days** |

### Detailed Tasks

#### Week 3 (continued): Models

**Day 1: Driver Interface**
```go
type UracDriver interface {
    GetUser(ctx context.Context, id string) (*User, error)
    GetUserByUsername(ctx context.Context, username string) (*User, error)
    ValidateUser(ctx context.Context, username, password string) (*User, error)
    GetUserGroups(ctx context.Context, userID string) ([]*Group, error)
    GetGroupACL(ctx context.Context, groupID string) (*ACL, error)
}
```

**Day 2-3: User Operations**
```
- User struct matching SOAJS schema
- MongoDB queries for user lookup
- Password validation (bcrypt)
- User status checking
```

**Day 4: Group Operations**
```
- Group struct and schema
- Group membership queries
- Nested group resolution
```

#### Week 4: Services & ACL

**Day 5-6: User Service**
```
- User authentication flow
- Token generation helpers
- Session management integration
- User caching
```

**Day 7: Group Service**
```
- Group hierarchy resolution
- Group-based permissions
- Caching layer
```

**Day 8-9: Permission Resolver**
```
- ACL merge logic
- Package permission resolution
- Environment-specific ACL
- API-level permissions
```

**Day 10: Integration Tests**
```
- Auth flow tests
- Permission calculation tests
- Edge case coverage
```

### Phase 2 Exit Criteria

- [ ] User authentication works
- [ ] Group membership resolves
- [ ] ACL calculation matches Node.js behavior
- [ ] All URAC tests pass
- [ ] Integration tests with real MongoDB pass

### Dependencies

```
Phase 1 ──────▶ Phase 2
   │
   ├── MongoDB client (required)
   ├── Configuration system (required)
   └── Logger (required)
```

---

## Phase 3: Gateway Middleware (Weeks 5-9)

### Objective
Implement the complete middleware pipeline for production-ready API gateway.

### Deliverables

| Component | Description | Est. Days |
|-----------|-------------|-----------|
| Provision system | Tenant/key loading | 3 |
| Key middleware | API key extraction | 1 |
| KeyACL middleware | Key permissions | 1.5 |
| Awareness middleware | Service discovery | 3 |
| URL middleware | URL parsing | 1 |
| Multi-tenant (MT) | Tenant resolution | 5 |
| OAuth middleware | Token validation | 2 |
| Traffic middleware | Rate limiting | 2 |
| GotoService | Request proxying | 4 |
| LastSeen middleware | Activity tracking | 1 |
| IP2Ban middleware | IP blacklist | 0.5 |
| End-to-end testing | Full pipeline tests | 5 |
| Performance testing | Benchmarks | 2 |
| Documentation | API docs, deployment | 2 |
| **Total** | | **33 days** |

### Detailed Tasks

#### Week 5: Provision & Key

**Day 1-3: Provision System**
```
- Tenant loading from MongoDB
- Product/package resolution
- Key decryption
- Caching with TTL
- Hot-reload support
```

**Day 4: Key Middleware**
```
- Extract key from header/query
- Key format validation
- Attach to context
```

**Day 5: KeyACL Middleware**
```
- Load key permissions
- Environment filtering
- ACL attachment
```

#### Week 6: Service Discovery

**Day 6-8: Awareness Middleware**
```
- Custom awareness mode
- HA (Kubernetes) mode
- Health check orchestration
- Service status tracking
- Load balancer integration
```

**Day 9: URL Middleware**
```
- URL parsing
- Path normalization
- Service name extraction
- Version extraction
```

#### Week 7: Multi-Tenant Core

**Day 10-14: MT Middleware**
```
- Tenant resolution from key
- Product/package selection
- ACL merge and validation
- OAuth integration point
- Service configuration injection
- Maintenance mode (per-tenant)
- The most complex component - thorough testing
```

#### Week 8: Supporting Middleware

**Day 15-16: OAuth Middleware**
```
- Token extraction
- Token validation
- User context population
- Refresh token handling
```

**Day 17-18: Traffic Middleware**
```
- Rate limiting (token bucket)
- Per-tenant limits
- MongoDB backend
- Memory backend option
```

**Day 19-22: GotoService (Proxy)**
```
- Target service resolution
- Request forwarding
- Header manipulation
- Response streaming
- Error handling
- Roaming support
- Connection pooling
```

**Day 23: Remaining Middleware**
```
- LastSeen - activity tracking
- IP2Ban - IP blacklist checking
- Enhancer - request enrichment
```

#### Week 9: Testing & Documentation

**Day 24-28: End-to-End Testing**
```
- Full request lifecycle tests
- Multi-tenant scenarios
- Error handling paths
- Performance regression tests
- Comparison with Node.js behavior
```

**Day 29-30: Performance Testing**
```
- Benchmark suite
- Load testing with k6
- Memory profiling
- CPU profiling
- Comparison metrics
```

**Day 31-33: Documentation**
```
- API documentation
- Deployment guide
- Migration guide
- Configuration reference
```

### Phase 3 Exit Criteria

- [ ] All 17 middleware components implemented
- [ ] Full request pipeline works end-to-end
- [ ] All integration tests pass
- [ ] Performance meets targets (30% latency improvement)
- [ ] Memory usage reduced by 50%+
- [ ] Documentation complete
- [ ] Ready for pilot deployment

### Dependencies

```
Phase 1 ──────▶ Phase 2 ──────▶ Phase 3
   │               │               │
   │               │               ├── MT (needs URAC)
   │               │               ├── OAuth (needs URAC)
   │               │               └── All other MW (needs Phase 1)
   │               │
   │               └── User/Group auth
   │
   └── MongoDB, Config, Logger, Registry, Basic MW
```

---

## Dependency Graph

```
┌─────────────────────────────────────────────────────────────────┐
│                     Dependency Graph                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────┐                                                    │
│  │ Config  │─────────────────────────────────────┐              │
│  └────┬────┘                                     │              │
│       │                                          │              │
│       ▼                                          ▼              │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐  ┌─────────┐        │
│  │ Logger  │───▶│ MongoDB │───▶│Registry │  │  HTTP   │        │
│  └────┬────┘    └────┬────┘    └────┬────┘  │ Server  │        │
│       │              │              │        └────┬────┘        │
│       │              │              │             │              │
│       │              ▼              ▼             │              │
│       │         ┌─────────┐   ┌─────────┐        │              │
│       │         │Provision│   │Awareness│        │              │
│       │         └────┬────┘   └────┬────┘        │              │
│       │              │              │             │              │
│       │              ▼              │             │              │
│       │         ┌─────────┐        │             │              │
│       └────────▶│  URAC   │◀───────┘             │              │
│                 └────┬────┘                      │              │
│                      │                           │              │
│                      ▼                           ▼              │
│                 ┌───────────────────────────────────┐           │
│                 │         Middleware Chain          │           │
│                 │  (context, cors, key, mt, etc.)   │           │
│                 └───────────────────────────────────┘           │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Risk Mitigation by Phase

### Phase 1 Risks

| Risk | Mitigation |
|------|------------|
| MongoDB driver differences | Create abstraction layer early |
| Config format incompatibility | Support both env vars and YAML |
| Logger format mismatch | Match exact SOAJS log format |

### Phase 2 Risks

| Risk | Mitigation |
|------|------------|
| Password hashing differences | Use same bcrypt parameters |
| ACL calculation bugs | Port test cases from Node.js |
| Session handling | Document differences clearly |

### Phase 3 Risks

| Risk | Mitigation |
|------|------------|
| MT complexity | Dedicated testing focus |
| Proxy performance | Use fasthttp for backend |
| Rate limit accuracy | Benchmark against Node.js |
| Subtle behavior differences | Comprehensive integration tests |

---

## Rollout Strategy

### Parallel Deployment

```
┌─────────────────────────────────────────────────────────────────┐
│                    Rollout Strategy                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Stage 1: Shadow Mode (Week 1-2 post-development)              │
│  ─────────────────────────────────────────────────              │
│  - Go version receives copy of traffic                          │
│  - Responses compared but not served                            │
│  - Identify behavior differences                                │
│                                                                 │
│  Stage 2: Canary (Week 3-4)                                    │
│  ─────────────────────────────────────────────────              │
│  - 5% traffic to Go version                                     │
│  - Monitor errors, latency, resource usage                      │
│  - Quick rollback capability                                    │
│                                                                 │
│  Stage 3: Gradual Rollout (Week 5-8)                           │
│  ─────────────────────────────────────────────────              │
│  - 5% → 25% → 50% → 75% → 100%                                 │
│  - Each stage: 1 week minimum                                   │
│  - Automatic rollback on error spike                            │
│                                                                 │
│  Stage 4: Full Migration                                       │
│  ─────────────────────────────────────────────────              │
│  - Decommission Node.js version                                 │
│  - Keep Node.js ready for emergency rollback (2 weeks)          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| p99 Latency | -30% | Continuous monitoring |
| Memory Usage | -50% | Container metrics |
| Error Rate | No increase | Error tracking |
| CPU Usage | -20% | Container metrics |
| Startup Time | -80% | Deployment metrics |
