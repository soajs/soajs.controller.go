# SOAJS Controller (Go)

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

The SOAJS API gateway written in Go. The Node.js gateway, [soajs.controller](https://github.com/soajs/soajs.controller) 4.3.28, is its specification. The Go gateway reads the same profile and the same databases, and answers requests as that gateway does.

**Status:**
- Every area of the Node.js gateway is ported:
  - the request chain (url, key, ACL, OAuth, urac, injectObj, proxy);
  - awareness and HA;
  - traffic, lastSeen and monitor;
  - the maintenance port;
  - `/soajs/acl`, `/soajs/proxy` and `/soajs/mcp` with its authorization flow;
  - idempotency and the response cache.
- The [conformance harness](test/conformance/README.md) replays 123 requests against both gateways on the same fixture database and compares status, headers and body byte for byte. Both pass all of them.
- The places where Go deliberately answers differently are listed in [docs/differences.md](docs/differences.md).

Go reads only the latest data schema ([docs/data-schema](docs/data-schema/README.md)). It carries no compatibility code for older shapes: a document that does not match its schema is logged and skipped or refused, never converted.

## Requirements

- Go 1.25 or later
- MongoDB, holding the SOAJS databases (provision, the environment's core databases)

## Build

```bash
make build                                   # bin/controller
go build -o bin/controller ./cmd/controller  # the same, without make
```

## Run

```bash
SOAJS_ENV=dev SOAJS_PROFILE=/opt/soajs/profile/soajsprofile ./bin/controller
```

- **`SOAJS_PROFILE`** is required. It is the profile file the Node.js gateway reads (`module.exports = {"name": "core_provision", "prefix": "", "servers": [...], ...}`), which says where the provision database is.
- **`SOAJS_ENV`** is the environment whose registry the gateway loads.
- **Ports:**
  - the gateway listens on `SOAJS_SERVICE_PORT` (4000);
  - the maintenance port is the registry's `serviceConfig.ports.controller` + `serviceConfig.ports.maintenanceInc`.

All settings are in [docs/configuration.md](docs/configuration.md).

## Test

```bash
make test                              # go test -race -cover ./...
test/conformance/run.sh go             # the Go gateway against the Node.js answers
```

The conformance harness needs a local MongoDB (`127.0.0.1:27017`, or `MONGO_URI`) and free ports 24000, 25000, 26000, 27000, 24100, 25100, 24200 and 25200. To record the Node.js answers again:

```bash
NODE_CONTROLLER_DIR=/path/to/soajs.controller test/conformance/run.sh node -record
```

Some unit tests use a local MongoDB too, in databases named `conformance_*` that they drop; they are skipped without one.

## Routes

**Gateway port:**

| Route | |
|---|---|
| `/<service>[/v<version>]/<path>` | Proxied to the service, after the key, ACL, OAuth and user checks of its version. |
| `/soajs/acl` | The caller's ACL, finalACL, packages, user and tenant. |
| `/soajs/proxy?proxyRoute=…[&__env=…]` | Forwards the request to a URL, or to another environment's gateway with the tenant's key for it. |
| `/soajs/mcp` | MCP (JSON-RPC 2.0): the APIs the caller may use, as tools. On when the custom registry `gateway.mcp.enabled` is true. |
| `/.well-known/oauth-protected-resource[/soajs/mcp]`, `/.well-known/{oauth-authorization-server,openid-configuration}/soajs/mcp/auth/<id>` | MCP authorization metadata, when `gateway.mcp.auth` is set. |

**Maintenance port:** `/heartbeat` (and any unknown path), `/reloadRegistry`, `/loadProvision`, `/getRegistry`, `/awarenessStat`, `POST /register`, `POST /cache/invalidate`, `/favicon.ico`.

How a request goes through the gateway: [docs/architecture.md](docs/architecture.md).

## Repository

| Path | |
|---|---|
| `cmd/controller` | The gateway. |
| `cmd/schemacheck` | Checks a database against the data schema (field names and types only). |
| `internal/` | The gateway's packages: middleware, registry, provision, awareness, urac, server. |
| `pkg/` | Types, the mongo client, key decryption, logging, version. |
| `docs/` | Configuration, architecture, differences from Node.js, data schema. |
| `test/conformance` | The conformance harness. |
| `deployments/`, `configs/` | Container, Kubernetes and configuration examples. The conformance harness does not cover them. |

## License

Apache 2.0, see [LICENSE](LICENSE). Third-party notices: [THIRD_PARTY.md](THIRD_PARTY.md).
