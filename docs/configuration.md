# Configuration

The gateway takes its settings from three places:
- the environment variables below;
- the profile file, which says where the provision database is;
- the registry it loads from that database for its environment. The registry decides everything else: databases, ports, awareness, OAuth, throttling, custom registry features.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `SOAJS_PROFILE` | required | The profile file, as the Node.js gateway reads it: `module.exports = {"name": "core_provision", "prefix": "", "servers": [{"host", "port"}], "credentials", "URLParam", ...}`. |
| `SOAJS_ENV` | `dev` | The environment whose registry is loaded. |
| `SOAJS_SERVICE_PORT` | `4000` | The gateway port. |
| `SOAJS_MAINTENANCE_PORT_INCREMENT` | `1001` | Maintenance port = gateway port + this, used only when the registry has no `serviceConfig.ports.controller` and `maintenanceInc`. |
| `SOAJS_SERVICE_NAME`, `SOAJS_SERVICE_VERSION`, `SOAJS_SERVICE_GROUP` | `controller`, `1`, `Gateway` | The gateway's own service: what `/soajs/acl` and `/soajs/mcp` are checked as, and the service MCP leaves out of the tools. |
| `SOAJS_SENSITIVE_ENVS` | `["dashboard"]` | A JSON array of environments whose tokens roam to other environments. |
| `SOAJS_DEPLOY_HA` | | Any value: HA mode (no hosts collection, no self-registration). `kubernetes`: hosts are read from the Kubernetes API, in the namespace of the registry's `deployer.container.kubernetes.namespace`. |
| `SOAJS_SRVIP` | the host's IPv4 | The IP the gateway registers under. |
| `SOAJS_SRV_AUTOREGISTERHOST` | | Set to anything but `true`: register as 127.0.0.1. |
| `SOAJS_MANUAL` | | Set: the `controllers` document is kept under the gateway's IP; otherwise under 127.0.0.1. |
| `SOAJS_MAX_INJECT_HEADER_SIZE` | `65536` | The largest `soajsinjectobj` header, in UTF-16 code units; a larger one answers 135. |
| `SOAJS_THROTTLE_CLEANUP_INTERVAL`, `SOAJS_IDEMPOTENCY_CLEANUP_INTERVAL`, `SOAJS_CACHE_CLEANUP_INTERVAL` | `60000` | How often (ms) the memory models of traffic, idempotency and the cache drop expired entries. |
| `SOAJS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `SOAJS_LOG_FORMAT` | `json` (`console` when `SOAJS_ENV` is `dev`) | `console` for readable logs. |
| `SOAJS_SERVER_SHUTDOWN_TIMEOUT` | `10` | Seconds to finish requests on SIGINT or SIGTERM. |

Any of the settings above can also be written in a `config.yaml` (keys `service_port`, `log_level`, `maintenance.port_increment`, ...), looked up in the working directory, `./configs` and `/etc/soajs/`. The environment variables take precedence.

## From the registry

The registry is the environment document with its databases (`dbs`, resolved against the `resources` clusters), the marketplace services and daemons, the hosts, the custom registry and `infra`. Its shapes are in [data-schema](data-schema/README.md). The gateway reads these parts of it:

- **`serviceConfig`:**
  - `ports.controller` and `ports.maintenanceInc` (the maintenance port);
  - `awareness` (health check interval, auto-reload of the registry);
  - `oauth`, `key` (extKey decryption), `cors`, `throttling`.
- **`coreDB`:** the provision, oauth and gateway databases. Access tokens are read in the `oauth` database when the environment has one, else in provision. Throttling, idempotency and the cache use the `gateway` database, else provision.
- **The custom registry:**
  - `gateway`: `traffic` (with `ip2ban`), `maintenanceMode`, `idempotency`, `cache`, `mcp`, `lastSeen`, `membership`, `mt.whitelist`, `oauth.deviceIdCheck`;
  - `oauth`: PIN wrapper and whitelist, roaming;
  - `urac`: `masterDB`, the core database that holds users and groups.

  [custom_registry.md](data-schema/custom_registry.md) describes each one.
- **`urac`:** users and groups are read in `coreDB[masterDB]` only. A missing `masterDB` is a configuration error: calls that need the user answer 146.

## Feature switches in the custom registry

| Entry | Turns on |
|---|---|
| `gateway.idempotency` | `Idempotency-Key` handling per service and API (memory or mongo model). |
| `gateway.cache` | GET answer caching per service and API, and `POST /cache/invalidate` (mongo model). |
| `gateway.mcp.enabled` | `/soajs/mcp`. With `gateway.mcp.auth`, the 401 challenge and the `/.well-known` metadata. |
| `gateway.traffic` | Throttling overrides and the model, and `ip2ban`; the strategies are in `serviceConfig.throttling`. |
| `gateway.lastSeen` | Reporting user activity to urac. |
| `gateway.maintenanceMode` | `on`: every request answers `status` (503) with `message`, and `Retry-After` when `retryAfter` is set. |
