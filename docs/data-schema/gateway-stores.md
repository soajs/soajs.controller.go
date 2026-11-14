# Gateway stores

Collections the gateway itself writes for throttling, idempotency and the response cache. They are used only when the custom registry selects `model: "mongo"` for the feature.

- **Database:** the environment's `gateway` core DB when it declares one, else provision. Node.js: server/controller.js:189-200.
- **Schemas:**
  - [throttling_monitor.schema.json](throttling_monitor.schema.json)
  - [throttling_error.schema.json](throttling_error.schema.json)
  - [idempotency_store.schema.json](idempotency_store.schema.json)
  - [cache_store.schema.json](cache_store.schema.json)
  - [cache_invalidation.schema.json](cache_invalidation.schema.json)
- **Read and written by** the gateway only. Node.js creates the indexes at startup, even when the model is `memory`.

## throttling_monitor

Requests counted in the current window. Node.js: mw/traffic/model/mongo.js.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `l1` | string | yes | Tenant id. |
| `l2` | string | yes | Client IP for a strategy of `type` 1, `_ALL_` otherwise. |
| `firstReqTime` | date | yes | Window start. |
| `count` | integer | yes | Requests in the window. |

**Index:** `{l1: 1, l2: 1}`. **Operations:**
- `findOne({l1, l2})`;
- upsert `$set {firstReqTime, count: 0}` when the window has expired;
- `$inc {count: 1}`.

## throttling_error

A request refused once its retries are exhausted. It has no index and no TTL.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `l1`, `l2` | string | yes | As above. |
| `ts` | date | yes | |
| `staregy` | string | | Strategy name. Node.js writes the field misspelled; the spec keeps the name as stored. |

## idempotency_store

Locks and recorded answers for `Idempotency-Key`. Node.js: mw/idempotency/model/mongo.js.

| Field | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `l1` | string | yes | | Tenant id. |
| `l2` | string | yes | UUID v4, either case | The `Idempotency-Key`, as sent. |
| `status` | string | yes | `in_flight` \| `completed` | In flight answers 409; completed replays the response. |
| `lockId` | string \| null | | | |
| `lockedAt` | date | | | |
| `completedAt` | date | when completed | | |
| `expiresAt` | date | yes | | |
| `response.statusCode`, `headers`, `body` | integer, object, string | when completed | | The answer to replay. Only answers below 500 are stored. |

**Indexes:** `{l1: 1, l2: 1}` unique, and a TTL index on `{expiresAt: 1}`.

## cache_store

Cached GET answers. Node.js: mw/cache/model/mongo.js.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `l1` | string | yes | Tenant id. |
| `l2` | string | yes | `<service>:GET:<api path>:<md5 of the sorted query without access_token, or empty>`, plus `:u:<user id>` for `tenant_user` scope. |
| `response.statusCode`, `headers`, `body` | integer, object, string | yes | Only 2xx answers are stored. |
| `cachedAt`, `expiresAt` | date | yes | |
| `service`, `api` | string | yes | `api` is the configured entry, e.g. `GET /orders/:id`. |
| `user` | string \| null | yes | |

**Indexes:** `{l1: 1, l2: 1}` unique, a TTL index on `{expiresAt: 1}`, and `{l1, service, api, user}`.

## cache_invalidation

Marks an invalidation, so an answer computed before it is not stored after it.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `l1`, `service` | string | yes | |
| `api`, `user` | string \| null | yes | `null` covers every api / user. |
| `clearedAt` | date | yes | |
| `expiresAt` | date | yes | `clearedAt` + 15 minutes. |

**Indexes:** `{l1, service, api, user}` unique, and a TTL index on `{expiresAt: 1}`.
