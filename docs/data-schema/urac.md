# urac: users and groups

The user record and its groups. When the token's `loginMode` is `urac` and `getUserFromToken` is off, the gateway reads the user and the user's groups on every private call.

- **Database:** `coreDB[masterDB]`, where `masterDB` is `custom_registry` `urac` `value.masterDB`. The database must be a `tenantSpecific:false` entry of `environment.dbs.databases`. **masterDB only**: users and groups live in one database so they can be aggregated. A missing `masterDB`, or one not in `coreDB`, is a configuration error.
- **Schemas:** [users.schema.json](users.schema.json), [groups.schema.json](groups.schema.json).
- **Written by** the urac service (model/mongo/user.js, group.js), and by the oauth service through soajs.urac.driver (social login, `lastLogin`). The gateway never writes.
- **Indexes:** created by urac. The gateway relies on lookups by `username`, `email` and `_id`, and on groups by `{tenant.id, code}`.

**DASHBOARD target configuration:**
- `custom_registry` `urac` `value.masterDB` = `"DBTN_urac"`;
- `environment` DASHBOARD `dbs.databases.DBTN_urac = {cluster, tenantSpecific: false}`.

With `dbs.config.prefix` `""`, `coreDB["DBTN_urac"]` is the existing `DBTN_urac` database. The live DASHBOARD environment does not match this yet; the user handles that configuration.

## users

**Read:** one `findOne` per call (soajs.urac.driver index.js:253-353; model/mongo/user.js):
- by `email` when the token's `username` looks like an email;
- else by `username`;
- by `_id` when the token has no username.

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | Injected as `urac._id`. |
| `username` | string | yes | | |
| `email`, `phone` | string | | | Compared with the token when `validateTokenAccount` is on (146). |
| `firstName`, `lastName` | string | | | Injected. |
| `password` | string | | bcrypt | Not used by the gateway; never injected. |
| `status` | string | yes | `active`, `inactive`, `pending`, `pendingNew`, `pendingJoin` | Only `active` passes: pending → 405, inactive → 406. The gateway answers 146 for both. |
| `tenant.id`, `tenant.code` | string | yes | | The user's home tenant. |
| `tenant.pin` | object | | | PIN state. |
| `groups` | string[] | yes | group codes | |
| `config.allowedTenants[]` | object | | `tenant{id, code, …}`, `groups` | Invitations: under an invited tenant, the user takes its `groups`. |
| `config.keys.<iKey>.config` | object | | | Merged over the key's configuration for this user. |
| `profile` | object | yes | free-form | Injected when the service version has `urac_Profile`. |
| `memberships[]` | object | | `product`, `membership` | Resolved through `custom_registry` `gateway` `membership`. |
| `ts`, `lastLogin`, `lastNetwork`, `locked`, `ln`, `socialId` | | | | Not used by the gateway. |

## groups

**Read:** `find({"tenant.id": <tenant id>, code: {$in: <user groups>}})` when the user has groups (soajs.urac.driver model/mongo/group.js:102-117).

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `code` | string | yes | | Group code. |
| `name` | string | yes | | |
| `description`, `locked`, `ts` | | | | Not used by the gateway. |
| `tenant.id`, `tenant.code` | string | yes | | The group's tenant. |
| `config.allowedPackages.<PRODUCT>` | string[] | yes (map may be empty) | package codes | Together across the user's groups: the packages whose ACL applies to the user under that product. |
| `config.allowedEnvironments.<ENV>` | object | | | Not used by the gateway. |

## Not part of this spec

- The per-tenant urac database `<prefix><tenant code>_urac` (`tenantMetaDB.urac`), `tenant.main`, `masterCode`, `dbCodes` and the roaming `tenantMetaDB` (soajs.urac.driver model/mongo/user.js:24-65, group.js:23-65).
- User `config.packages`, which no current writer produces.

## Example

```json
{
  "_id": {"$oid": "<user id>"},
  "username": "<username>",
  "email": "<email>",
  "firstName": "<first>",
  "lastName": "<last>",
  "password": "<bcrypt hash>",
  "status": "active",
  "tenant": {"id": "<tenant id>", "code": "ACME"},
  "groups": ["owner"],
  "config": {},
  "profile": {},
  "ts": 1794666321241
}
```

```json
{
  "_id": {"$oid": "<group id>"},
  "code": "owner",
  "name": "Owner",
  "tenant": {"id": "<tenant id>", "code": "ACME"},
  "config": {"allowedPackages": {"SHOP": ["SHOP_DEFAULT"]}}
}
```

## Live data

Checked in DBTN_urac only; the `og` databases could not be read with the access used.
- users: 14 documents, 13 match. One has `config.packages`.
- groups: 3 documents, all match.
