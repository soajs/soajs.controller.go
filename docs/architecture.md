# Architecture

The gateway is a chain of middlewares on a chi router, in the order of the Node.js gateway (`server/controller.js`). Each one is a port of the Node.js middleware named in the table. Registry, provision and awareness are loaded at startup and kept in memory. The maintenance port is a separate HTTP server.

## The request chain

| # | Middleware (`internal/middleware`) | Node.js | What it does |
|---|---|---|---|
| 1 | Context | soajs | The request context; removes an inbound `soajsinjectobj`. |
| 2 | CORS | mw/cors | From the registry's `serviceConfig.cors`. |
| 3 | Favicon | mw/favicon | `/favicon.ico`. |
| 4 | Response | mw/response | The SOAJS answer envelope. |
| 5 | Enhancer | mw/enhancer | Device and geo details from the request headers. |
| 6 | IP2Ban | mw/ip2ban | Refuses the IPs of the custom registry `traffic.ip2ban`. |
| 7 | MaintenanceMode | mw/maintenanceMode | With the custom registry `gateway.maintenanceMode.on`, answers every request with its status (503) and message. |
| 8 | WellKnown | mw/soajsRoutes/mcp/auth.js wellKnown | MCP authorization metadata. |
| 9 | URL | mw/url, lib/parseURL.js | Service, version and path, parsed as WHATWG `new URL` does (130, 136). |
| 10 | Awareness, AwarenessEnv | mw/awareness, mw/awarenessEnv | Hosts in this and other environments. |
| 11 | MCPAuthorize | mw/soajsRoutes/mcp/auth.js authorize | `/soajs/mcp`: the 401 challenge; the key of a token restricted to one key. |
| 11a | CallEvents | mw/events | With the custom registry `gateway.events`: one call event per request from here on, written off the request path. |
| 12 | Key | mw/key | Resolves the extKey (148, 144), the network package, the package. |
| 13 | KeyACL | mw/keyACL | The final ACL of the requested version (154). |
| 14 | GotoService | mw/gotoService | The service and its version (130, 132); `/soajs/acl`, `/soajs/proxy`, `/soajs/mcp`; passport login, roaming, redirect. |
| 15 | MT | mw/mt | The key's tenant (152, 153), mt whitelist, geo (155) and device (156) checks, whether OAuth is required. |
| 16 | OAuth | mw/oauth | The access token, deviceId (156), restrictedTo (147), PIN (145), with oauth2-server's answers. |
| 17 | Urac | mw/mt urac.js | The logged-in user (146), roaming (170), the user's ACL and config. |
| 18 | MTACL | mw/mt utils.js serviceCheck, apiCheck | Service and API access (154, 158, 159, 161). |
| 19 | InjectObj | mw/mt | The `soajsinjectobj` of a key's API (135 when too large). |
| 20 | Idempotency | mw/idempotency | `Idempotency-Key` (180, 181, 182). |
| 21 | Traffic | mw/traffic | Throttling (429). |
| 22 | Cache | mw/cache | GET answer cache (`X-Cache`). |
| 23 | LastSeen | mw/lastSeen | Reports user activity. |
| 24 | the route handler | mw/gotoService redirectToService, simpleRTS, roaming, proxyRequest | Calls what GotoService chose: the service (request timeout and renewals, 133, 134, 135), or the gateway's own route. |

## Packages

| Package | |
|---|---|
| `internal/server` | The two HTTP servers, the chain, the maintenance routes. |
| `internal/middleware` | The chain above, plus MCP (`mcp.go`, `mcpauth.go`, `swagger.go`, `ordered.go`), idempotency and cache models, the monitor. |
| `internal/registry` | Loads and builds the registry from the environment, resources, marketplace, hosts and custom registry; `/register`; other environments' registries. |
| `internal/provision` | Keys, packages, tenants and access tokens, cached with their `_TTL`. |
| `internal/awareness` | Health checks, HA (Kubernetes), round-robin, self-registration and the `controllers` document. |
| `internal/urac` | The user record, its ACL, tenant access, the access resolver, membership. |
| `internal/dbconfig` | The profile and the database configurations. |
| `internal/dataschema` | Validates documents against `docs/data-schema`. |
| `internal/lib` | URL parsing, client IP, path patterns, versions. |
| `internal/uaparser` | The device check, running ua-parser-js as Node.js does. |
| `internal/soajserr` | The Node.js error table. |

## Stores the gateway writes

Only with the mongo models, in the environment's `gateway` database, else provision ([gateway-stores.md](data-schema/gateway-stores.md)):
- `throttling_monitor` and `throttling_error`;
- `idempotency_store`;
- `cache_store` and `cache_invalidation`.

With call events on the `mongo` sink, `call_events` in the core database `gateway.events.mongo.db` names ([call_events.md](data-schema/call_events.md)).

The gateway also writes its `controllers` document, its host and, on `/register`, the marketplace and hosts.
