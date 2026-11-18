# How Go reads imperfect data

The schemas in this directory are the latest shape only. Real data is not always in it: a field written by an older console, a value of the wrong type, a field a service added. One bad field must not take down a whole tenant, product, service or registry entry. Where the Node.js gateway keeps working, Go keeps working, and never with more access than Node.js gives.

Go reads every document in this order:

1. **Node.js readings first.** Where the Node.js code gives a malformed value a definite meaning, Go reads it the same way. Each reading below cites the Node.js line it follows (soajs.controller 4.3.30 unless named). Where Node.js passes a value through (injected into a service, or dumped by the maintenance port), Go passes the raw value too, and makes its own decisions on the reading.
2. **Old shapes are left out toward refusal.** A field that Node.js still reads with an access meaning, but that this spec does not support, is never ignored. The piece that holds it is left out, so the gateway refuses what that piece would have allowed.
3. **Unknown fields are ignored.** A field the spec does not list is not read and is never a reason to leave anything out. The one exception is a token's `user.restrictedTo`: an unknown restriction refuses the token.
4. **The smallest broken piece is left out.** A value that still does not match the schema leaves out the smallest piece holding it, and the rest of the document is read. The piece is chosen toward refusal, never toward access, and never in a way that makes the gateway fall back to a wider default.
5. **Every problem is visible.** Each document's problems are logged at error, at most once per document per load, and listed on the maintenance route [`/schemaProblems`](#schemaproblems). `cmd/schemacheck` lists the same problems from the database ([schemacheck](#schemacheck)).

## Pieces, by collection

| Collection | Left out, smallest first | The document is skipped when |
|---|---|---|
| `tenants` | an extKey, then a key, then an application; a field the gateway does not use (`name`, `description`, `profile`, `tenant`, an extKey's `label`) on its own | its `_id`, `code` or `type` cannot be read |
| `products` | under an `apisPermission: "restricted"` version, the API rule (that API is then refused, 159); anything else in a service's ACL leaves out `acl.<env>.<service>` for the package (154); an unreadable environment ACL or package is left out (refused) | its `_id` or `code` cannot be read, or `packages` is not a list |
| `marketplace` | an older version | its `_id`, `name`, `type`, `configuration.port` or `versions` cannot be read, or its **latest** version cannot be read (130): calls without a version never go to an older version, which may have other code and ACL |
| `custom_registry` `gateway` | one feature (`events`, `mcp`, `lastSeen`, …); in `traffic`, one leaf (`model`, `throttling`), and one ip2ban address; in `maintenanceMode`, a field other than `on` | its `_id`, `name`, `created` or `value` cannot be read |
| `custom_registry` `urac` | one access resolver, one `autoRoaming` tenant, or another setting | `masterDB` cannot be read, or it has `masterCode` or `dbCodes` |
| `custom_registry` `oauth` | `roaming` | as above |
| `custom_registry`, any | one `sharedEnvs` environment | |
| `environment` | only fields that do not decide access: `description`, `locked`, `settings`, `services.config.logger`, `services.config.awareness`, `services.controller.requestTimeout`, `requestTimeoutRenewal`, `maxPoolSize` | any other field cannot be read: the gateway never falls back to a default wider than the data (an origin of `*`, the default key password, the default CORS methods). The registry is then not built; on a reload, the previous one stays |
| `users` | one invitation (`config.allowedTenants[]` item), one membership; `groups` (falls back to the key's package, see below) | its `_id`, `username`, `status`, `tenant` or a `config.keys` entry cannot be read: a `config.keys` entry overrides the key's configuration |
| `groups` | one product's `config.allowedPackages` that is neither a list nor a string (falls back to the key's package, see below); `config.allowedEnvironments` | anything else cannot be read: the group gives no packages (falls back to the key's package) |
| `oauth_token` | nothing | anything cannot be read: a token is used whole or not at all |
| `resources`, `hosts`, `infra` | nothing | anything cannot be read |

A document that matches the schema but does not decode into the gateway's types is skipped and reported with its `_id` (`decode: …`). The schema types every field the gateway decodes, so this marks a gap in the schema.

### Keys of a tenant read again

The gateway caches keys. When a tenant is read again (its key's `_TTL` passed, an extKey is not in the cache, or `/loadProvision`), the keys it no longer gives are forgotten: an application or key left out, a tenant that cannot be read, or a key no tenant has any more. Such a key answers as Node.js answers a missing key (153), never with the old entry.

### Refusal, not "missing", for users

Node.js gives a user whose groups name no loadable package no user ACL, and the key's package ACL then applies. A piece Go left out must never turn into that fallback:

- a package, or its ACL for the environment, that Go could not read makes the user's merged ACL empty: every service is refused (154);
- a service Go left out of one package is removed from the user's merged ACL, even when another package has it: merged, that package's `apisPermission: "restricted"` could otherwise be lost.

These are the package's own rules. A user's roles (its groups, and a group's `allowedPackages`) that cannot be read fall back to the key's package instead, as Node.js does: see [below](#nodejs-behaviour-that-is-wider-than-the-data-intends).

A package that could not be read is cached for a minute (`UnreadableTTL`), so a fix applies without `/loadProvision`.

## Node.js readings

| Collection | Field | Read as | Node.js |
|---|---|---|---|
| `tenants` | `extKeys[].device`, `geo`: `{}` or missing | no restriction, as `null` | mw/mt/utils.js:416-427, 446-490 |
| `tenants` | `extKeys[].expDate`: missing | never expires | soajs.core.modules soajs.provision/index.js:56 |
| `tenants` | `locked` | `!!locked` | soajs.core.modules soajs.core/provision/mongo.js:310 |
| `tenants` | `oauth.disabled` | truthiness, as `0` or `1` | mw/mt/utils.js:559 |
| `tenants`, `products` | `_TTL`: a numeric string; anything else not a number | its number; reloaded after a minute (Node.js: a NaN `_TTL` is never fresh) | soajs.core.modules soajs.provision/index.js:201-205 |
| `products` | a version's `access`: missing or not a boolean | `false` when missing, else truthiness | mw/mt/utils.js:848 |
| `products` | a version's `apisPermission`: anything but `"restricted"` | not restricted | mw/mt/utils.js:214, 883 |
| `products` | a method: `null`, `false`, `0`, `""` | no method level | mw/keyACL/index.js:74 |
| `products` | a version without any method | the version's `access` for every API of the version | mw/keyACL/index.js:96-97; mw/mt/utils.js:883-893 |
| `products` | a method without `apis` | no API rules | mw/keyACL/index.js:79 |
| `products` | an API rule's `access`: missing or not a boolean | `false` (public) when missing, else truthiness | mw/mt/utils.js:225-228 |
| `marketplace` | `configuration.group`, `requestTimeout`, `requestTimeoutRenewal`: falsy | `"service"`, `30`, `0` | modules/registry/index.js:187-190 |
| `marketplace` | a version's `extKeyRequired`, `oauth`, `urac`, `urac_*`, `tenant_Profile`, `provision_ACL`: missing or not a boolean | truthiness; the registry keeps a truthy value as it is, a falsy one as `false` | modules/registry/index.js:201-211 (`value \|\| false`) |
| `marketplace` | a version's `interConnect`: `null` or falsy | no peers | modules/registry/index.js:212; mw/mt/index.js:318 |
| `marketplace` | a version's `maintenance`: falsy | none | modules/registry/index.js:213 |
| `custom_registry` | `sharedEnvs.<ENV>` | truthiness, kept as stored | modules/registry/mongo.js:40, 52 |
| `custom_registry` `gateway` | `maintenanceMode`: not an object | off | mw/maintenanceMode/index.js:18 |
| `custom_registry` `gateway` | `maintenanceMode.on`, kept as stored; `status`, `message`, `retryAfter` falsy | truthiness; `503`, the default message, no Retry-After | mw/maintenanceMode/index.js:18-27 |
| `custom_registry` `gateway` | `traffic.ip2ban`: not a list; an address that is not a string | no ban; never matches | mw/ip2ban/index.js:43 |
| `custom_registry` `gateway` | `traffic.model`, `traffic.throttling`: any shape | as Node.js reads them: an unknown model turns throttling off; a service entry that is not an object, or whose strategy is not a name, gives no override or no throttling; `apis` not a list applies to every API | mw/traffic/index.js:150-185 |
| `custom_registry` `gateway` | `mt.whitelist.ips`: not a list; `acl`, `oauth`, kept as stored | no whitelist; truthiness | mw/mt/utils.js:269-291 |
| `custom_registry` `gateway` | `oauth.deviceIdCheck`, kept as stored | on unless `false` | mw/oauth/index.js:54-60 |
| `environment` | `services.controller.authorization` | truthiness; dumped as stored | mw/gotoService/redirectToService.js:306 |
| `environment` | `services.config.awareness.autoRegisterService` | truthiness; dumped as stored | modules/registry/index.js:428 |
| `environment` | `services.config.cors.enabled` | truthiness | mw/cors/index.js:17 |
| `environment` | `services.config.cors.credentials` | truthiness. Node.js writes the value itself as the header, `"true"` when falsy; Go writes `"true"` | mw/cors/index.js:20 |
| `environment` | `services.config.cors.origin`: falsy; a list of one string; anything else not a string | `*`; that origin; **no `Access-Control-Allow-Origin`** (Node.js sends the value as it is, which browsers refuse) | mw/cors/index.js:19, 26 |
| `environment` | `services.config.cors.methods`, `headers`: a list of strings | one header, comma separated (Node.js sends one line per item) | mw/cors/index.js:29-37 |
| `environment` | `services.config.cors.maxage`, a throttling strategy's `type`, `window`, `limit`, `retries`, `delay`: a numeric string | its number | mw/cors/index.js:23, 30; mw/traffic |
| `environment` | `services.config.oauth.getUserFromToken` | truthiness; dumped as stored | mw/mt/urac.js:41 |
| `environment` | `dbs.databases.<name>.tenantSpecific` | truthiness | modules/registry/index.js:86 |
| `users` | `config.allowedTenants`: not a list | no invitations | mw/mt/urac.js:81-85 |
| `groups` | `config.allowedPackages.<PRODUCT>`: a string | kept as it is, and merged as the urac driver merges: the first group's value as it is, then `[...new Set([...a, ...b])]`, where a string gives its characters. A string alone is not a list of packages, so the key's package ACL applies | soajs.urac.driver lib/user.js:299-316; mw/mt/urac.js:153-180 |
| `users`, `groups` | any field the gateway does not read | ignored, and injected with `config` as Node.js injects it | |

## Old shapes, left out toward refusal

| Collection | Shape | Left out | Node.js reads it at |
|---|---|---|---|
| `tenants` | an application's `acl` or `acl_all_env` | the application | mw/keyACL/index.js:26-29 |
| `products` | `access`, `apis`, `apisRegExp` or `apisPermission` directly under a service (no versions) | the service ACL | mw/keyACL/index.js:34-44 |
| `products` | `apis` or `apisRegExp` at the version level | the service ACL | mw/keyACL/index.js:97 |
| `products` | `apisPermission` or `apisRegExp` at the method level | the service ACL | mw/keyACL/index.js:85-92 |
| `products` | a version's `access` as a list of groups | the service ACL | mw/mt/utils.js:853-863 |
| `products` | an API rule's `access` as a list of groups | the rule under `restricted`, else the service ACL | mw/mt/utils.js:232-243 |
| `custom_registry` `urac` | `masterCode`, `dbCodes` | the entry | |
| `oauth_token` | `userId`, `tId` (oauth_urac) | the token | |

## Node.js behaviour that is wider than the data intends

Go matches Node.js here. It is listed so the data can be fixed:

- **A user's roles that cannot be read** fall back to the key's package, by decision: the gateway follows Node.js and reports it. A user whose `groups` cannot be read, a group that cannot be read, or a group's `allowedPackages` entry that is not a list gives no packages; when nothing else gives one, the user has no user ACL and the key's package ACL applies, which may allow more than the roles intended. A readable group next to them still gives its packages. Each is logged at error once per document per load and listed on `/schemaProblems` as `fell back to key package`.
- **A group whose only package does not exist:** the user has no user ACL, so the key's package ACL applies, which may allow more than the group intended (mw/mt/urac.js:173-180; soajs.provision index.js:184-198). With several packages, the missing one is skipped and the others are merged. Missing packages are reported on `/schemaProblems` as data problems.
- **An API rule without `access`** is public, and **a version without methods** opens every API of the version at the version's `access`, as Node.js reads them.

## Known differences in the readings

- A CORS origin that is not a string: Node.js sends it, and browsers refuse it; Go sends no `Access-Control-Allow-Origin`.
- A CORS `credentials` of `"false"`: Node.js sends `false`, Go sends `true` (Go has always sent `true`).
- A `gotoService` feature left out turns request monitoring off.

## /schemaProblems

`GET /schemaProblems` on the maintenance port (Go only) lists the current problems of the documents the gateway has read. `?collection=<name>` narrows the list.

```json
{
  "result": true,
  "ts": 1794666000000,
  "service": {"service": "soajs.controller", "version": "4.4.32", "type": "rest", "route": "/schemaProblems"},
  "data": {
    "documents": 2,
    "actions": {"dropped piece": 1, "read-as-node": 1},
    "problems": [
      {"collection": "marketplace", "_id": "<id>", "path": "/versions/0/interConnect", "rule": "soajs.controller modules/registry/index.js:212-213, mw/mt/index.js:318 (a falsy interConnect is none)", "action": "read-as-node"},
      {"collection": "tenants", "_id": "<id>", "path": "/applications/0/keys/0/extKeys/0/device", "rule": "/properties/applications/…/device/oneOf", "action": "dropped piece", "piece": "/applications/0/keys/0/extKeys/0"}
    ]
  }
}
```

| Field | |
|---|---|
| `collection`, `_id` | The document. `_id` is the hex of an ObjectId; any other `_id` is quoted, so it never takes an ObjectId's place. |
| `path` | JSON pointer of the field. A map key that is a key is hidden: a user's `config.keys.<internal key>` is `/config/keys/<redacted>`. |
| `rule` | The schema keyword that failed, the Node.js line a reading follows, or `decode: …` with the field and the types. |
| `action` | `read-as-node`, `ignored field`, `dropped piece`, `old shape`, `skipped document`, `fell back to key package` (a user's roles that cannot be read), or `data problem` (a reference to something that does not exist). |
| `piece` | What was left out, when it is larger than `path`. |

Values are never listed, only locations: no key, extKey, token, secret or password.

How long a problem stays listed:

- provision documents (tenants, products): until the next full load (`/loadProvision`, and at start), plus those read since;
- registry documents: until the next registry build of the gateway's environment;
- documents read per request (users, groups, tokens) and data problems: an hour after their last read, and at most 1000 of them, the oldest dropped first;
- a document read again without problems leaves the list at once.

## schemacheck

`cmd/schemacheck` reads a database the way the gateway does and lists, per collection, the documents read whole, read with problems, and that cannot be read. `-explain` prints each problem as `/schemaProblems` would. It exits 1 when a document cannot be read. It reads only, and never prints values or the connection string.

```bash
SOAJS_MONGO_URI=... schemacheck -provision core_provision -oauth oauth -urac <masterDB> -explain
```

`-strict` checks the latest shape only, as before: any document that does not match is listed as invalid.

schemacheck decodes into generic documents, not the gateway's types. Since the schemas type every field the gateway decodes, a decode failure (`decode: …` on `/schemaProblems`) shows a gap in the schema and is fixed there.
