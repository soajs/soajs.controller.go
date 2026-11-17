# marketplace

The catalog: one item per service, daemon, resource, static or custom item, with its versions. The gateway reads `service` and `mdaemon` items.

- **Database:** provision. **Schema:** [marketplace.schema.json](marketplace.schema.json).
- **Read by the gateway** at each registry load: `find({type: "service"})` and `find({type: "mdaemon"})` (Node.js modules/registry/mongo.js:189, 200). The item named `controller`, and the gateway's own name, are skipped (modules/registry/index.js:184).
- **Written by:**
  - the marketplace service: catalog upsert by `{name, type}`, branch and version updates (soajs.marketplace model/mongo/marketplace.js). The repositories service syncs catalogs from git through it.
  - the gateway, on `POST /register` with `mw` and at its own startup (modules/registry/mongo.js:305-368).
- **Indexes:** none that the gateway relies on.

## Fields of a `service` item

| Path | Type | Required | Values / default | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `name` | string | yes | | Service name, the first segment of the URL. |
| `type` | string | yes | `service` | |
| `description` | string | | | Not used by the gateway. |
| `configuration.group` | string | | `service` | |
| `configuration.port` | integer | yes | | Service port. Unique per type. |
| `configuration.requestTimeout` | integer (s) | | `30` | Time before a request is timed out. |
| `configuration.requestTimeoutRenewal` | integer | | `0` | Times the timeout is renewed. |
| `configuration.subType` | string | | | Not used by the gateway. |
| `versions[]` | object | yes, at least one | | |
| `versions[].version` | string | yes | integer string, e.g. `"1"` | |
| `versions[].extKeyRequired` | boolean | | `false` | A key is required; runs the multi-tenant checks. |
| `versions[].oauth` | boolean | | `false` | Private APIs need a token. |
| `versions[].urac` | boolean | | `false` | Injects the user. |
| `versions[].urac_Profile`, `urac_ACL`, `urac_Config`, `urac_GroupConfig` | boolean | | `false` | Injects these parts of the user. |
| `versions[].tenant_Profile` | boolean | | `false` | Injects the tenant profile. |
| `versions[].provision_ACL` | boolean | | `false` | Injects the package ACL. |
| `versions[].interConnect` | array \| null | | missing or `null`: no peers | Peers the service calls directly. |
| `versions[].interConnect[].name` | string | yes | | Peer service. |
| `versions[].interConnect[].version` | string | | integer string; absent means the peer's latest | |
| `versions[].maintenance.readiness` | string | yes | e.g. `/heartbeat` | Health-check path. |
| `versions[].maintenance.port.type` | string | yes | `maintenance` (service port + maintenanceInc) \| `inherit` (service port) | |
| `versions[].maintenance.commands[]` | object | | `label`, `path`, `icon` | Not used by the gateway. |
| `versions[].apis[]` | object | | | The service's APIs. |
| `versions[].apis[].v` | string | yes | | API path, `:param` segments allowed. |
| `versions[].apis[].m` | string | yes | `get`, `post`, `put`, `delete`, `patch`, `head`, `options` | Method. |
| `versions[].apis[].l` | string | yes | | Label. |
| `versions[].apis[].group`, `groupMain` | string, boolean | | | |
| `versions[].soa`, `lastSync`, `branches`, `tags`, `documentation`, `swagger`, `profile`, `customByEnv` | | | | Not used by the gateway. Node.js reads `customByEnv` but never applies it (modules/registry/index.js:202-214). |
| `src`, `metadata`, `settings`, `ui`, `deploy` | | | | Not used by the gateway. |

## Fields of an `mdaemon` item

| Path | Type | Required | Meaning |
|---|---|---|---|
| `configuration.group`, `port` | string, integer | yes | |
| `versions[].version` | string | yes | Integer string. |
| `versions[].maintenance.readiness`, `port.type` | string | yes | As for services. |
| `versions[].jobs` | object | | Not used by the gateway. |

Other item types (`static`, `resource`, `custom`, `daemon`, `config`) are not read by the gateway, and the schema only checks `_id`, `name`, `type`, `configuration` and `versions`.

How a service that does not match is read, piece by piece (an unreadable latest version skips the service): [imperfect-data.md](imperfect-data.md).

## Not part of this spec

- Type `endpoint` items, `src.url(s)` and `apibuilder` (D6) (modules/registry/index.js:193-197; gotoService/lib/preRedirect.js:127-148).
- Root-level `version`, `extKeyRequired` and `oauth` (modules/registry/index.js:224-234).
- `versions` as an object.
- Dotted versions such as `"1.0"`.
- `interConnect: null`. No interConnect is an empty array.
- A version without `maintenance`.
- Daemon `configuration.maintenance` (the old place).
- `apis` entries without `m`.

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "type": "service",
  "name": "orders",
  "description": "Orders",
  "configuration": {"group": "Commerce", "port": 4100, "requestTimeout": 30, "requestTimeoutRenewal": 5, "subType": "soajs"},
  "src": {"provider": "github", "owner": "<owner>", "repo": "<repo>"},
  "versions": [{
    "version": "1",
    "extKeyRequired": true, "oauth": true,
    "urac": true, "urac_Profile": false, "urac_ACL": false, "urac_Config": false, "urac_GroupConfig": false,
    "tenant_Profile": false, "provision_ACL": false,
    "interConnect": [{"name": "payments", "version": "1"}],
    "maintenance": {"readiness": "/heartbeat", "port": {"type": "maintenance"}, "commands": []},
    "apis": [{"l": "Checkout", "v": "/checkout", "m": "post", "group": "Orders"}]
  }]
}
```
