# hosts

The running hosts of each service, one per service, version and host.

- **Database:** provision. **Schema:** [hosts.schema.json](hosts.schema.json).
- **Read by the gateway**, outside HA mode, at each registry load: `find({env: <env lowercase>})` (Node.js modules/registry/mongo.js:210-224). It builds `services.<name>.hosts.<version>` and `hosts.latest`. Unless the environment is sensitive, the host's `port` replaces the service port (modules/registry/index.js:273-305).
- **Read for other environments' gateways** (awarenessEnv): `find({name: <gateway name>, env: {$ne: <env>}})` (modules/registry/mongo.js:296-304).
- **Written by** the gateway: its own host at startup, and services that call `POST /register`. Upsert on `{env, name, version, ip, hostname}` (modules/registry/mongo.js:262-286).
- **Indexes:** none created.

## Fields

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `env` | string | yes | lowercase env code | |
| `name` | string | yes | a marketplace service name | |
| `version` | string | yes | integer string, e.g. `"1"` | Service version. |
| `ip` | string | yes | | Host address. |
| `port` | integer | yes | | Service port on that host. |
| `hostname` | string | yes | lowercase | Part of the upsert key. |
| `type` | string | | | Not used by the gateway. |
| `serviceHATask` | string | | | Not used by the Go gateway (HA registers no hosts). |
| `healthy`, `lastCheck` | boolean, date | | | Not used by the gateway. |

Not part of this spec: a host without `version`, which Node.js reads as `"1"` (modules/registry/index.js:284-286).

## Example

```json
{"_id": {"$oid": "<id>"}, "env": "dev", "name": "orders", "version": "1", "ip": "10.0.0.12", "port": 4100, "hostname": "orders-7d9f"}
```


---

## controllers

The gateway's snapshot of its awareness: each service's hosts and their health. **Written only**; nothing reads it.

- **Database:** provision. **Schema:** [controllers.schema.json](controllers.schema.json).
- **Written by** the gateway: `updateOne({env, ip}, {$set: {data, ts}}, {upsert: true})`. This happens after the first health check, on SIGINT, and on `/awarenessStat?update` (Node.js modules/registry/mongo.js:370-457).

| Path | Type | Required | Meaning |
|---|---|---|---|
| `env` | string | yes | Lowercase env code. |
| `ip` | string | yes | The gateway host. |
| `ts` | number (ms) | yes | |
| `data.services`, `data.daemons` | object | yes | The registry entries with their `hosts` and `awarenessStats`. Keys that MongoDB cannot store are encoded: a `.` in a version becomes `x`, and in an IP becomes `_dot_`. |
