# oauth_token

Access and refresh tokens issued by the oauth service. The gateway reads access tokens.

- **Database:** the environment's `oauth` core DB when it declares one, else provision. Node.js: server/controller.js:106-113.
- **Schema:** [oauth_token.schema.json](oauth_token.schema.json).
- **Read by the gateway:** `findOne({token: <token>, type: "accessToken"})` on every private call and on `/soajs/mcp` (Node.js soajs.core/provision/mongo.js:70-98; mw/soajsRoutes/mcp/auth.js:298).
  - A token is accepted when `env` is the gateway's environment, or a sensitive environment (`SOAJS_SENSITIVE_ENVS`, default `dashboard`).
- **Written by** the oauth service (login, refresh, `/restricted/token/*`), through soajs.core.modules provision (mongo.js:113-138). The gateway never writes it.
- **Indexes the gateway relies on:** `{token: 1, type: 1}`, and a TTL index on `{expires: 1}` with `expireAfterSeconds: 0`. Node.js creates both on the oauth connection (mongo.js:62-65).

## Fields

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `token` | string | yes | 40 hex characters | |
| `type` | string | yes | `accessToken` \| `refreshToken` | |
| `clientId` | string | yes | tenant id | The tenant the token was issued under. A different request tenant means roaming. |
| `env` | string | yes | lowercase env code | Environment the token was issued in. |
| `expires` | date | yes | | Refused after this time. |
| `user` | object | yes | | The user, as at issue time. |
| `user._id` | ObjectId | yes | | |
| `user.id` | string | yes | 24 hex characters | `_id` as a string. |
| `user.username` | string | yes | | |
| `user.loginMode` | string | yes | `urac` \| `oauth` | `urac`: the gateway reads the user from urac on each call, unless `getUserFromToken`. `oauth`: the token's user is the user record, the default for restricted and guest tokens. |
| `user.tenant.id`, `user.tenant.code` | string | yes | | The user's tenant. |
| `user.tenant.pin` | object | | | Not used by the gateway. |
| `user.deviceId` | string \| null | yes | | When a string, it must equal the `device-id` header (156). `null`: the token is not bound to a device. |
| `user.agent` | string \| null | | | Not used by the gateway (see `restrictedTo.agent`). |
| `user.restrictedTo` | object | | | A restricted token: each key present must match the request (147). |
| `user.restrictedTo.tenant` | string \| string[] | yes | tenant id | |
| `user.restrictedTo.product`, `package`, `key`, `env`, `agent` | string \| string[] | | `env` is case-insensitive; a single `key` also supplies the key on `/soajs/mcp` | |
| `user.guest` | boolean | | | Guest token: an identity with no urac record. |
| `user.groups` | string[] | | | |
| `user.config`, `groupsConfig`, `profile`, `email`, `phone`, `firstName`, `lastName`, `memberships`, `socialLogin` | | | | Used as the user record when the token's user is used (see `loginMode`). |
| `user.pinLogin`, `pinLocked` | boolean | | | PIN state. |

Other `user` fields are allowed: the oauth service copies the urac user into the token.

## Not part of this spec

- The `oauth_urac` user shape `{userId, tId}` (mt/urac.js:242-247).
- A token without `user.deviceId`, which Node.js accepts as "created before deviceId was introduced" (oauth/index.js:69-73). No device is `null`.
- Looking up the token in the provision DB after the oauth DB (soajs.core/provision/mongo.js:78-92). The environment's `dbs` decide the database.

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "type": "accessToken",
  "token": "<40 hex>",
  "clientId": "<tenant id>",
  "env": "dev",
  "expires": {"$date": "2026-11-14T12:00:00Z"},
  "user": {
    "_id": {"$oid": "<user id>"},
    "id": "<user id>",
    "username": "<username>",
    "loginMode": "oauth",
    "tenant": {"id": "<tenant id>", "code": "ACME"},
    "groups": ["owner"],
    "deviceId": "<device id>",
    "agent": "<user agent>",
    "restrictedTo": {"tenant": "<tenant id>", "product": "SHOP"}
  }
}
```
