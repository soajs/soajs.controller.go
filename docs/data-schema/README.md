# Gateway data schema

This is the spec of every MongoDB collection the gateway reads or writes. The Go gateway reads **one shape per field, the latest one**, and nothing else.

Two things make it up:
- each `<collection>.schema.json` file (JSON Schema draft 2020-12) is used by the Go tests and by `cmd/schemacheck`;
- the `.md` pages explain the fields.

The shapes come from the Node.js gateway's code (soajs.controller 4.3.28 and the soajs.core.modules 5.2.22 and soajs.urac.driver 3.2.20 it bundles) and from the code of the services that write each collection. They were checked read-only against a production dataset; the results are kept outside this repository.

## Rules

1. **Latest shape only.** The compatibility branches Node.js keeps for older data are not part of this spec. Each page lists the ones it leaves out.
2. **Imperfect data is read as Node.js reads it, piece by piece.** A document that does not match is not skipped whole: the Node.js readings apply first, then the smallest piece that still does not match is left out, toward refusal, never toward access. Old shapes are left out, never converted. Fields the spec does not list are ignored. Every problem is logged and listed on the maintenance route `/schemaProblems`. See [imperfect-data.md](imperfect-data.md).
3. Fields the gateway does not use are listed as **not used by the gateway**. Most are allowed and not constrained, so the services that own them (console, oauth, urac, …) can change them.
4. Placeholders such as `<id>` and `<extKey>` in the examples stand for real values. No real tenant, user, key or secret appears in these pages.
5. **Users and groups are open.** Their services add fields of their own (Connect writes `lastVisit`, `joinedAt` and `via` in `config.allowedTenants[]`), so a users or groups document, and every object in it, accepts fields this spec does not list, and only the fields the gateway reads are checked. The same goes for a token's `user`. See [urac.md](urac.md).

## Databases

| Kind (`x-soajs.database`) | Name | Notes |
|---|---|---|
| `provision` | `<profile prefix><profile name>`, e.g. `core_provision` | From the profile file (`SOAJS_PROFILE`). Node.js: modules/registry/mongo.js:22, 233-260. |
| core DB `<name>` | `<prefix><name>` for each `environment.dbs.databases.<name>` with `tenantSpecific:false` | `<prefix>` is `dbs.databases.<name>.prefix` when set, else `dbs.config.prefix`. The cluster comes from the `resources` record of type `cluster` with that name. Node.js: modules/registry/index.js:43-115. |
| `oauth` | core DB `oauth` when the environment declares it, else `provision` | The environment's `dbs` decide; there is no lookup in one database and then the other. Node.js: server/controller.js:106-113; soajs.core/provision/mongo.js:42-50. |
| `gateway` | core DB `gateway` when declared, else `provision` | Node.js: server/controller.js:189-200. |
| `urac` | core DB `<masterDB>`, where `masterDB` is `custom_registry` `urac` `value.masterDB` | **masterDB only.** It is a configuration error if `masterDB` is missing or is not a core DB of the environment. Not part of this spec: the tenant-specific `<code>_urac` database, `tenant.main`, `dbCodes`, `masterCode`, the roaming `tenantMetaDB` (soajs.urac.driver model/mongo/user.js:24-65, group.js:23-65). |

The driver joins prefix and name with no separator (soajs.mongo/index.js:1509-1514).

## Collections

| Collection | Database | Gateway use | Read by the gateway | Written by | Page |
|---|---|---|---|---|---|
| environment | provision | registry | yes | console | [environment.md](environment.md) |
| infra | provision | deployer configuration (kubernetes) | yes | infra | [infra.md](infra.md) |
| resources | provision | database clusters | yes | console | [resources.md](resources.md) |
| custom_registry | provision | gateway, urac and oauth settings | yes | console | [custom_registry.md](custom_registry.md) |
| marketplace | provision | services, mdaemons and their versions | yes | marketplace (catalog sync through repositories); the gateway on `/register` | [marketplace.md](marketplace.md) |
| hosts | provision | service hosts | yes | the gateway (`/register`, its own host) | [hosts.md](hosts.md) |
| controllers | provision | awareness snapshot | no (write only) | the gateway | [hosts.md](hosts.md#controllers) |
| tenants | provision | keys, tenant, tenant oauth | yes | multitenant | [tenants.md](tenants.md) |
| products | provision | packages and their ACL | yes | multitenant | [products.md](products.md) |
| oauth_token | oauth | access tokens | yes | oauth | [oauth_token.md](oauth_token.md) |
| users, groups | urac | the user record and its groups | yes | urac; oauth (social login, lastLogin) | [urac.md](urac.md) |
| throttling_monitor, throttling_error, idempotency_store, cache_store, cache_invalidation | gateway | throttling, idempotency, response cache | yes | the gateway | [gateway-stores.md](gateway-stores.md) |
| call_events | the core database `gateway.events.mongo.db` names | call events | no (write only) | the gateway | [call_events.md](call_events.md) |

Not used by the gateway:
- `oauth_urac` and `daemon_grpconf`: Node.js only creates indexes on them.
- `services`, `settings`, `git`, `catalogs`, `templates`, `favorite`, `sessions`, `tokens`: never touched.
- lastSeen, soamonitor and the tenant access resolver: reached over HTTP, not through MongoDB.

## Validator

```bash
SOAJS_MONGO_URI=... go run ./cmd/schemacheck -provision core_provision -oauth oauth -urac urac -gateway gateway
```

- It only reads.
- It prints, per collection and database, the number of documents and how many match, followed by the `_id` of each document that does not.
- `-explain` adds the field location and the schema rule that failed, never the values.
- The connection string is read from the environment and never printed.

`internal/dataschema` holds the same validation for Go tests. `TestConformanceFixture` checks the conformance fixture against these schemas.
