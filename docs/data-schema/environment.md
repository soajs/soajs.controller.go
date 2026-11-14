# environment

One document per environment. It holds the gateway's registry: its ports, oauth, cors and awareness settings, its databases and its deployer.

- **Database:** provision. **Schema:** [environment.schema.json](environment.schema.json).
- **Read by the gateway** at startup, on `/reloadRegistry` and on the automatic reload: `findOne({code: <ENV>})` (Node.js modules/registry/mongo.js:68). The code is `SOAJS_ENV` uppercased.
- **Written by** the console: insert from a template (soajs.console bl/environment.js:67-150); `dbs`, `services` and `settings` updates (model/mongo/registry.js).
- **Indexes:** none that the gateway relies on.

## Fields

| Path | Type | Required | Values / default | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `code` | string | yes | uppercase `^[A-Z0-9]+$` | Environment code. |
| `description` | string | | | Not used by the gateway. |
| `locked` | boolean | | | Not used by the gateway. |
| `domain` | string | yes | | API domain. Used to build `protocol://[apiPrefix.]domain:port` for the cross-env `/soajs/proxy` (proxyRequest.js:112-116). |
| `apiPrefix`, `sitePrefix` | string | | | API and site sub-domains. |
| `port` | integer | yes | | Public port. |
| `protocol` | string | yes | `http` \| `https` | Public protocol. |
| `deployer` | object | yes | one of the two shapes below | How services are deployed. |
| `deployer.type` | string | yes | `container` \| `manual` | |
| `deployer.selected` | string | yes | `container.kubernetes` with type `container`, `manual` with type `manual` | |
| `deployer.container.kubernetes.id` | string | for kubernetes | 24-hex `_id` of an [infra](infra.md) document | The cluster account. |
| `deployer.container.kubernetes.namespace` | string | for kubernetes | | Namespace of the environment's services. |
| `deployer.manual.nodes` | string | | | Not used by the gateway. |
| `dbs.config.prefix` | string | yes | may be `""` | Prefix of every database of the environment. |
| `dbs.databases.<name>` | object | | | A database of the environment. `<name>` is also the core DB name, e.g. `oauth`, `gateway`, `users`. |
| `dbs.databases.<name>.cluster` | string | yes | name of a `resources` document of type `cluster` | Where the database lives. |
| `dbs.databases.<name>.tenantSpecific` | boolean | yes | | `false`: a core DB (`coreDB.<name>`). `true`: not used by the Go gateway (see [urac](urac.md)). |
| `dbs.databases.<name>.prefix` | string | | overrides `dbs.config.prefix` for this database | Part of the latest shape: an optional per-database prefix. |
| `services.controller.authorization` | boolean | yes | | Turns on the `soajsauth` session cookie (gotoService/redirectToService.js:305-306). |
| `services.controller.requestTimeout` | integer (s) | yes | | Gateway entry's request timeout. |
| `services.controller.requestTimeoutRenewal` | integer | yes | | Gateway entry's timeout renewals. |
| `services.config.ports.controller` | integer | yes | | Gateway port. |
| `services.config.ports.maintenanceInc` | integer | yes | | Maintenance port = port + maintenanceInc, for the gateway and every service. |
| `services.config.ports.randomInc` | integer | yes | | Not used by the gateway. |
| `services.config.awareness.autoRegisterService` | boolean | yes | | Whether the gateway registers its own host. |
| `services.config.awareness.autoRelaodRegistry` | integer (ms) | yes | | Automatic registry reload interval. The field name is spelled `autoRelaodRegistry`. |
| `services.config.awareness.cacheTTL` | integer (ms) | yes | | HA: how long the kubernetes service cache lives. |
| `services.config.awareness.healthCheckInterval` | integer (ms) | yes | | Service health-check interval. |
| `services.config.awareness.maxLogCount` | integer | yes | | Failed checks logged per host. |
| `services.config.key.algorithm` | string | yes | e.g. `aes256` | extKey cipher. |
| `services.config.key.password` | string | yes | secret | extKey passphrase. |
| `services.config.logger.level` | string | yes | `trace`, `debug`, `info`, `warn`, `error`, `fatal` | Log level. |
| `services.config.logger.src` | boolean | yes | | |
| `services.config.logger.gke`, `force` | boolean | | | |
| `services.config.logger.formatter.levelInString`, `outputMode` | boolean, string | | | |
| `services.config.cors.enabled` | boolean | yes | | CORS on or off. |
| `services.config.cors.origin`, `methods`, `headers` | string | yes | | CORS response headers. |
| `services.config.cors.credentials` | boolean | yes | | `Access-Control-Allow-Credentials`. |
| `services.config.cors.maxage` | integer (s) | yes | | |
| `services.config.oauth.getUserFromToken` | boolean | yes | | `true`: the token's user is the user record, with no urac lookup. |
| `services.config.oauth.accessTokenLifetime`, `refreshTokenLifetime` | integer (s) | yes | | Read by the oauth service; passed on to services. |
| `services.config.oauth.grants`, `debug` | string[], boolean | yes | | Not used by the Go gateway. |
| `services.config.cookie.secret` | string | yes | secret | Not used by the gateway. Passed on to services. |
| `services.config.session.name`, `secret` | string | yes | secret | The `soajsauth` session cookie. |
| `services.config.session.cookie`, `resave`, `rolling`, `saveUninitialized`, `unset` | | | | Not used by the gateway. Passed on to services. |
| `services.config.throttling.publicAPIStrategy`, `privateAPIStrategy` | string | | name of a strategy below | Default strategy for public and private APIs. |
| `services.config.throttling.<strategy>` | object | | | A throttling strategy. |
| `services.config.throttling.<strategy>.type` | integer | yes | `0` per tenant, `1` per tenant and IP | |
| `services.config.throttling.<strategy>.window` | integer (ms) | yes | | |
| `services.config.throttling.<strategy>.limit`, `retries`, `delay` | integer | yes | `delay` in ms | |
| `settings` | object | | | Not used by the gateway (console ACL). |

`services.config` is passed on to services through `/getRegistry`, without `cors` and `oauth`.

## Not part of this spec

These are Node.js compatibility shapes, refused by the schema:
- the old deployer without `id` (`nodes`, `apiProtocol`, `apiPort`, `auth.token`, `namespace.default`) (modules/registry/mongo.js:117-141);
- the three-part `selected` and the `namespace.{default, perService}` object (awareness/ha.js:229-237);
- `dbs.clusters` (modules/registry/index.js:54-58, 139-143);
- a database with inline servers (index.js:76-85);
- `dbs.session` and `dbs.config.session` (index.js:117-167); the Go gateway has no session database;
- `custom` embedded in the environment (modules/registry/mongo.js:178-185);
- `endpoints` (D6);
- `profile`, `sensitive`, `resources` embedded;
- `services.config.oauth.type`, `secret`, `algorithms`, `audience` (JWT tokens, D5).

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "code": "DEV",
  "description": "Development",
  "domain": "example.com",
  "apiPrefix": "api",
  "sitePrefix": "site",
  "port": 443,
  "protocol": "https",
  "deployer": {
    "type": "container",
    "selected": "container.kubernetes",
    "container": {"kubernetes": {"id": "<infra id>", "namespace": "dev"}}
  },
  "dbs": {
    "config": {"prefix": "dev_"},
    "databases": {
      "oauth": {"cluster": "<cluster name>", "tenantSpecific": false},
      "gateway": {"cluster": "<cluster name>", "tenantSpecific": false},
      "users": {"cluster": "<cluster name>", "tenantSpecific": false, "prefix": "shared_"}
    }
  },
  "services": {
    "controller": {"authorization": true, "requestTimeout": 30, "requestTimeoutRenewal": 0},
    "config": {
      "awareness": {"autoRegisterService": true, "autoRelaodRegistry": 3600000, "cacheTTL": 3600000, "healthCheckInterval": 5000, "maxLogCount": 5},
      "key": {"algorithm": "aes256", "password": "<secret>"},
      "logger": {"level": "error", "src": false, "gke": true},
      "cors": {"enabled": true, "origin": "*", "credentials": true, "methods": "GET,HEAD,PUT,PATCH,POST,DELETE", "headers": "key,soajsauth,device-id,Content-Type", "maxage": 1728000},
      "oauth": {"grants": ["password", "refresh_token"], "debug": false, "getUserFromToken": false, "accessTokenLifetime": 7200, "refreshTokenLifetime": 1209600},
      "ports": {"controller": 4000, "maintenanceInc": 1000, "randomInc": 100},
      "cookie": {"secret": "<secret>"},
      "session": {"name": "soajsID", "secret": "<secret>", "cookie": {"path": "/", "httpOnly": true, "secure": false, "maxAge": null}, "resave": false, "saveUninitialized": false, "rolling": false, "unset": "keep"},
      "throttling": {"publicAPIStrategy": "open", "open": {"type": 1, "window": 60000, "limit": 50, "retries": 2, "delay": 1000}}
    }
  }
}
```
