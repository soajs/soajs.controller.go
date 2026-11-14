# custom_registry

Named settings per environment. The gateway reads three entries: `gateway`, `urac` and `oauth`. All other entries belong to services and are passed on to them untouched.

- **Database:** provision. **Schema:** [custom_registry.schema.json](custom_registry.schema.json).
- **Read by the gateway** at each registry load: `find({$or: [{created: <ENV>, plugged: true}, {plugged: true, shared: true}]})`. An entry is kept when `created` is the environment or `sharedEnvs[<ENV>]` is `true`. The result is keyed `custom[name]` (Node.js modules/registry/mongo.js:48-57, 178).
- **Written by** the console (model/mongo/customRegistry.js).
- **Indexes:** none that the gateway relies on.

## Fields of every entry

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `name` | string | yes | | Entry name. |
| `created` | string | yes | uppercase env code | Owning environment. |
| `plugged` | boolean | yes | | Only plugged entries are loaded. |
| `shared` | boolean | yes | | |
| `sharedEnvs.<ENV>` | boolean | yes (map may be empty) | | |
| `value` | object (or string for other entries) | yes | | The settings. |
| `author`, `locked`, `settings` | | | | Not used by the gateway. |

## `gateway`: `value`

Every block is optional.

| Path | Type | Required in its block | Values / default | Meaning |
|---|---|---|---|---|
| `traffic.model` | string | yes | `memory` \| `mongo` | Throttling store. |
| `traffic.ip2ban` | string[] | | exact IPs | Refused with 403 `banned`. |
| `traffic.throttling.<service>` | object | | | Per-service strategy, overriding the environment's. |
| `traffic.throttling.<service>.publicAPIStrategy`, `privateAPIStrategy` | string | | strategy name | |
| `traffic.throttling.<service>.apis` | string[] | | relative API paths | Limits the override to these APIs. |
| `idempotency.model` | string | yes | `memory` \| `mongo` | Idempotency store. |
| `idempotency.enforce` | boolean | | default `false` | `Idempotency-Key` required for every enabled API. |
| `idempotency.services.<service>.enabled` | boolean | yes | | |
| `idempotency.services.<service>.enforce` | boolean | | | Overrides the global `enforce` (428 when the key is missing). |
| `idempotency.services.<service>.ttl` | integer (ms) | | default 60000 | Lock and replay lifetime. |
| `idempotency.services.<service>.apis` | string[] | | `"METHOD /path/:param"`; all APIs when absent | |
| `cache.model` | string | yes | `memory` \| `mongo`; invalidation needs `mongo` | Response cache store. |
| `cache.defaultTTL` | integer (ms) | | default 300000 | |
| `cache.services.<service>.enabled` | boolean | yes | | |
| `cache.services.<service>.apis.<"GET /path">` | object | | | A cached API. |
| `….enabled` | boolean | yes | | |
| `….scope` | string | | `tenant` \| `tenant_user`; default `tenant` for public APIs, `tenant_user` for private | Cache key scope. |
| `….ttl` | integer (ms) | | default `defaultTTL` | |
| `lastSeen.active` | boolean | yes | | Reports user activity to urac. |
| `lastSeen.include.<service>.apis.<path>` | string[] | yes | HTTP methods; the path is relative, `"*"` matches all | APIs that count as activity. |
| `lastSeen.serviceName`, `serviceVersion`, `api` | string | | default `urac`, `"3"`, `/user/last/seen` | Where activity is reported. |
| `lastSeen.network` | string | | | Sent with the report; also the key of the network package override. |
| `lastSeen.targets[]` | object | | | Extra report targets. |
| `lastSeen.targets[].serviceName`, `api` | string | yes | | |
| `lastSeen.targets[].serviceVersion`, `method` | string | | method default `POST` | |
| `membership.active` | boolean | yes | | |
| `membership.<MAIN PRODUCT>` | string[] | | product codes | Products whose users' membership is that of the main product. |
| `mcp.enabled` | boolean | yes | | Turns `/soajs/mcp` on. |
| `mcp.ttl`, `errorTtl`, `timeout`, `maxTools` | integer | | 300000, 30000, 10000, 300 | |
| `mcp.auth.scheme` | string | | `http` \| `https` | Scheme of the addresses the gateway gives out. |
| `mcp.auth.default`, `byProduct.<product>`, `byHost.<host>` | object | | | Authorization-server endpoints. |
| `….authorization_endpoint`, `token_endpoint` | string | yes | | |
| `….registration_endpoint` | string | | | |
| `maintenanceMode.on` | boolean | yes | | Answers every request with the status below. |
| `maintenanceMode.status`, `message`, `retryAfter` | integer, string, integer | | 503, `Maintenance mode is on, come back soon` | |
| `gotoService.renewReqMonitorOff` | boolean | | | Turns off the request-timeout renewal. |
| `gotoService.monitor.blacklist`, `whitelist` | string[] | | service names; whitelist wins | Requests reported to soamonitor. |
| `gotoService.monitor.req_info`, `req_header`, `req_query`, `req_body`, `req_response` | boolean | | | What is reported. |
| `gotoService.monitor.buffer.time`, `limit` | integer | | ms, count | Batch reports. |
| `mt.whitelist.ips` | string[] | yes | CIDR | IP whitelist. |
| `mt.whitelist.acl`, `oauth` | boolean | | | Skip the ACL or oauth check for whitelisted IPs. |
| `oauth.deviceIdCheck` | boolean | | default on; `false` turns it off | Token `deviceId` check. |

## `urac`: `value`

| Path | Type | Required | Meaning |
|---|---|---|---|
| `masterDB` | string | **yes** | The core DB holding users and groups: `coreDB[masterDB]`. It must be a `tenantSpecific:false` database of the environment. |
| `tenantAccessResolvers.<name>` | object | | Cross-tenant access resolver: `service`, `api` (required), `version`, `method` (default `post`), `timeout` (default 2000 ms). |
| `autoRoaming.<tenant id>.<home tenant id>.groups` | string[] | | Groups given to a roaming user. |

Refused: `masterCode` and `dbCodes`, which belong to the per-tenant urac database (see [urac](urac.md)).

## `oauth`: `value`

| Path | Type | Meaning |
|---|---|---|
| `pinWrapper.servicename`, `apiname` | string | The API allowed while the PIN is not entered. |
| `pinWhitelist.<service>.<method>.apis`, `regex` | string[] | More APIs allowed while the PIN is not entered. |
| `roaming.services.<service>.whitelistips` | string[] | IPs allowed to use `soajsroaming`. |

## Not part of this spec

- An entry without the `value` wrapper.
- `lastSeen` without `include`, which Node.js applies to every request (lastSeen/index.js:61-63).
- `include` API keys given as full paths `/service/api` (lastSeen/index.js:84-87).

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "name": "gateway",
  "created": "DEV",
  "plugged": true,
  "shared": false,
  "sharedEnvs": {},
  "value": {
    "traffic": {"model": "mongo", "ip2ban": [], "throttling": {"orders": {"publicAPIStrategy": "strict", "apis": ["/checkout"]}}},
    "idempotency": {"model": "mongo", "services": {"orders": {"enabled": true, "ttl": 86400000, "apis": ["POST /checkout"]}}},
    "lastSeen": {"active": true, "include": {"orders": {"apis": {"/checkout": ["post"]}}}},
    "mcp": {"enabled": true, "maxTools": 300, "auth": {"scheme": "https", "default": {"authorization_endpoint": "https://portal.example.com/authorize", "token_endpoint": "https://api.example.com/token"}}}
  }
}
```

## Live data

73 documents; 69 match.
- 3 `gateway` entries have `lastSeen` without `include`.
- 1 `urac` entry, DASHBOARD's, has no `masterDB`. DASHBOARD's target configuration is `masterDB` `"DBTN_urac"` (see [urac](urac.md)).
