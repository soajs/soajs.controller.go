# resources

Shared resources. The gateway uses those of type `cluster`: the MongoDB clusters its databases live on.

- **Database:** provision. **Schema:** [resources.schema.json](resources.schema.json).
- **Read by the gateway** at each registry load: `find({$or: [{created: <ENV>, plugged: true}, {plugged: true, shared: true}]})`. A document is kept when `created` is the environment or `sharedEnvs[<ENV>]` is `true`. The result is keyed `resources[type][name]` (Node.js modules/registry/mongo.js:32-46, 168).
- **Written by** the console (model/mongo/resource.js).
- **Indexes:** none that the gateway relies on.

## Fields

| Path | Type | Required | Values / default | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `name` | string | yes | | Referenced by `environment.dbs.databases.<db>.cluster`. |
| `type` | string | yes | `cluster`, `server`, `cdn`, `system`, `authorization`, `other` | Only `cluster` is used. |
| `category` | string | yes | e.g. `mongo`, `mongodb srv` | Not used by the gateway. |
| `created` | string | yes | uppercase env code | Owning environment. |
| `plugged` | boolean | yes | | Only plugged resources are loaded. |
| `shared` | boolean | yes | | |
| `sharedEnvs.<ENV>` | boolean | yes (map may be empty) | | Environments a shared resource is given to. |
| `config` | object | yes | for `cluster`, below | |
| `config.protocol` | string | | `mongodb://` \| `mongodb+srv://` | |
| `config.servers[]` | object | yes, at least one | | |
| `config.servers[].host` | string | yes | | |
| `config.servers[].port` | integer | | absent with `mongodb+srv://` | |
| `config.credentials` | object | | | Absent for a cluster without authentication. When present, both fields are required. |
| `config.credentials.username`, `password` | string | yes | secret | |
| `config.URLParam` | object | yes (may be empty) | | Connection-string options, e.g. `maxPoolSize`, `retryWrites`, `writeConcern`. |
| `config.streaming` | object | | | Not used by the gateway. |
| `config.extraParam` | object | | must be empty | The old driver option groups are not part of this spec. |
| `author`, `locked`, `settings` | | | | Not used by the gateway. |

## Not part of this spec

- Cluster records without a `config` wrapper (Node.js modules/registry/index.js:69-75).
- `extraParam` option groups (`db`, `server`, `replSet`, `mongos`) and the old driver options Node.js deletes before connecting (soajs.mongo/index.js:1590, 1681-1693).
- `credentials` with an empty `username` meaning "no credentials" (soajs.mongo/index.js:1530-1534). A cluster without authentication has no `credentials`.

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "name": "main_cluster",
  "type": "cluster",
  "category": "mongodb srv",
  "created": "DEV",
  "plugged": true,
  "shared": true,
  "sharedEnvs": {"STG": true},
  "config": {
    "protocol": "mongodb+srv://",
    "servers": [{"host": "<cluster host>"}],
    "credentials": {"username": "<user>", "password": "<password>"},
    "URLParam": {"maxPoolSize": 10, "retryWrites": true, "writeConcern": {"w": "majority"}},
    "streaming": {}
  }
}
```

## Live data

3 documents; all match.
