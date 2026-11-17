# How Go reads imperfect data

The schemas in this directory are the latest shape only. Real data is not always in it: a field written by an older console, a value of the wrong type, a field a service added. One bad field must not take down a whole tenant, product, service or registry entry. Where the Node.js gateway keeps working, Go keeps working, and never with more access than Node.js gives.

Go reads every document in this order:

1. **Node.js readings first.** Where the Node.js code gives a malformed value a definite meaning, Go reads it the same way. Each reading below cites the Node.js line it follows (soajs.controller 4.3.30 unless named).
2. **Old shapes are left out toward refusal.** A field that Node.js still reads with an access meaning, but that this spec does not support, is never ignored. The piece that holds it is left out, so the gateway refuses what that piece would have allowed.
3. **Unknown fields are ignored.** A field the spec does not list is not read and is never a reason to leave anything out. The one exception is a token's `user.restrictedTo`: an unknown restriction refuses the token.
4. **The smallest broken piece is left out.** A value that still does not match the schema leaves out the smallest piece holding it, and the rest of the document is read. The piece is chosen toward refusal, never toward access.
5. **Every problem is visible.** Each document's problems are logged at error, at most once per document per load, and listed on the maintenance route [`/schemaProblems`](#schemaproblems).

## Pieces, by collection

| Collection | Left out, smallest first | The document is skipped when |
|---|---|---|
| `tenants` | an extKey, then a key, then an application; a field the gateway does not use (`name`, `description`, `profile`, `tenant`, an extKey's `label`) on its own | its `_id`, `code` or `type` cannot be read |
| `products` | under an `apisPermission: "restricted"` version, the API rule (that API is then refused, 159); anything else in a service's ACL leaves out `acl.<env>.<service>` for the package (154); an unreadable environment ACL or package is left out (refused) | its `_id` or `code` cannot be read, or `packages` is not a list |
| `marketplace` | an older version | its `_id`, `name`, `type`, `configuration.port` or `versions` cannot be read, or its **latest** version cannot be read (130): calls without a version never go to an older version, which may have other code and ACL |
| `custom_registry` `gateway` | one feature (`events`, `mcp`, `lastSeen`, …); inside `traffic`, one throttling strategy or ip2ban address; inside `maintenanceMode`, a field other than `on` | its `_id`, `name`, `created` or `value` cannot be read |
| `custom_registry` `urac` | one access resolver, one `autoRoaming` tenant, or another setting | `masterDB` cannot be read, or it has `masterCode` or `dbCodes` |
| `custom_registry` `oauth` | one setting (`pinWrapper`, `pinWhitelist`, `roaming`) | as above |
| `environment` | the field | its `_id` or `code` cannot be read, or a field the gateway needs is left out: the registry is then not built |
| `users` | one invitation (`config.allowedTenants[]` item), one `config.keys` entry, one membership | its `_id`, `username`, `status`, `tenant` or `groups` cannot be read. Without its groups the user would get the key's package ACL |
| `groups` | `config.allowedEnvironments` | anything else cannot be read. A group whose packages cannot be read refuses the user everything (see below) |
| `oauth_token` | nothing | anything cannot be read: a token is used whole or not at all |
| `resources`, `hosts`, `infra` | nothing | anything cannot be read |

### Refusal, not "missing", for users

Node.js gives a user whose groups name no loadable package no user ACL, and the key's package ACL then applies. A piece Go left out must never turn into that fallback:

- a package, or its ACL for the environment, that Go could not read makes the user's merged ACL empty: every service is refused (154);
- a service Go left out of one package is removed from the user's merged ACL, even when another package has it: merged, that package's `apisPermission: "restricted"` could otherwise be lost;
- a group whose `allowedPackages` cannot be read, or that cannot be read at all, makes the user's ACL empty.

## Node.js readings

| Collection | Field | Read as | Node.js |
|---|---|---|---|
| `tenants` | `extKeys[].device`, `geo`: `{}` or missing | no restriction, as `null` | mw/mt/utils.js:416-427, 446-490 |
| `tenants` | `extKeys[].expDate`: missing | never expires | soajs.core.modules soajs.provision/index.js:56 |
| `tenants` | `locked` | JavaScript truthiness | mw/mt/index.js:218 (injected) |
| `tenants` | `oauth.disabled` | truthiness, as `0` or `1` | mw/mt/utils.js:559 |
| `tenants` | `oauth.pin.<product>`: not an object; `enabled`: missing or not a boolean | no PIN; truthiness | mw/mt/utils.js:629 |
| `products` | a version's `access`: missing or not a boolean | `false` when missing, else truthiness | mw/mt/utils.js:848 |
| `products` | a version's `apisPermission`: anything but `"restricted"` | not restricted | mw/mt/utils.js:214, 883 |
| `products` | a method: `null`, `false`, `0`, `""` | no method level | mw/keyACL/index.js:74 |
| `products` | a version without any method | the version's `access` for every API of the version | mw/keyACL/index.js:96-97; mw/mt/utils.js:883-893 |
| `products` | a method without `apis` | no API rules | mw/keyACL/index.js:79 |
| `products` | an API rule's `access`: missing or not a boolean | `false` (public) when missing, else truthiness | mw/mt/utils.js:225-228 |
| `marketplace` | `configuration.group`, `requestTimeout`, `requestTimeoutRenewal`: falsy | `"service"`, `30`, `0` | modules/registry/index.js:187-190 |
| `marketplace` | a version's `extKeyRequired`, `oauth`, `urac`, `urac_*`, `tenant_Profile`, `provision_ACL`: missing or not a boolean | `false` when missing, else truthiness | modules/registry/index.js:201-211 |
| `marketplace` | a version's `interConnect`: `null` or falsy | no peers | modules/registry/index.js:212; mw/mt/index.js:318 |
| `marketplace` | a version's `maintenance`: falsy | none | modules/registry/index.js:213 |
| `custom_registry` | `sharedEnvs.<ENV>` | truthiness | modules/registry/mongo.js:40, 52 |
| `custom_registry` `gateway` | `maintenanceMode`: not an object | off | mw/maintenanceMode/index.js:18 |
| `custom_registry` `gateway` | `maintenanceMode.on`; `status`, `message`, `retryAfter` falsy | truthiness; `503`, the default message, no Retry-After | mw/maintenanceMode/index.js:18-27 |
| `custom_registry` `gateway` | `traffic.ip2ban`: not a list; an address that is not a string | no ban; never matches | mw/ip2ban/index.js:43 |
| `custom_registry` `gateway` | `mt.whitelist.ips`: not a list; `acl`, `oauth` | no whitelist; truthiness | mw/mt/utils.js:269-291 |
| `custom_registry` `gateway` | `oauth.deviceIdCheck`: not a boolean | on (it is off only when `false`) | mw/oauth/index.js:54-60 |
| `environment` | `services.controller.authorization` | truthiness | mw/gotoService/redirectToService.js:306 |
| `environment` | `services.config.awareness.autoRegisterService` | truthiness | modules/registry/index.js:428 |
| `environment` | `services.config.cors.enabled` | truthiness | mw/cors/index.js:17 |
| `environment` | `services.config.cors.credentials` | truthiness. Node.js writes the value itself as the header, `"true"` when falsy; Go writes `"true"` | mw/cors/index.js:20 |
| `environment` | `services.config.oauth.getUserFromToken` | truthiness | mw/mt/urac.js:41 |
| `environment` | `dbs.databases.<name>.tenantSpecific` | truthiness | modules/registry/index.js:86 |
| `users` | `config.allowedTenants`: not a list | no invitations | mw/mt/urac.js:81-85 |
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

- **A group whose only package does not exist:** the user has no user ACL, so the key's package ACL applies, which may allow more than the group intended (mw/mt/urac.js:173-180; soajs.provision index.js:184-198). With several packages, the missing one is skipped and the others are merged. Missing packages are reported on `/schemaProblems` as data problems.
- **An API rule without `access`** is public, and **a version without methods** opens every API of the version at the version's `access`, as Node.js reads them.

## Known differences in the readings

- `tenants.locked`, `marketplace` flags and the environment flags are injected and dumped as booleans; Node.js passes a truthy value through as it is.
- A `gotoService` feature left out turns request monitoring off.
- A throttling strategy left out means no throttling for the keys and APIs that name it.

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
      {"collection": "marketplace", "_id": "<id>", "path": "/versions/0/interConnect", "rule": "soajs.controller modules/registry/index.js:212, mw/mt/index.js:318 (a falsy interConnect is none)", "action": "read-as-node"},
      {"collection": "tenants", "_id": "<id>", "path": "/applications/0/keys/0/extKeys/0/device", "rule": "/properties/applications/…/device/oneOf", "action": "dropped piece", "piece": "/applications/0/keys/0/extKeys/0"}
    ]
  }
}
```

| Field | |
|---|---|
| `collection`, `_id` | The document. |
| `path` | JSON pointer of the field. |
| `rule` | The schema keyword that failed, or the Node.js line a reading follows. |
| `action` | `read-as-node`, `ignored field`, `dropped piece`, `old shape`, `skipped document`, or `data problem` (a reference to something that does not exist). |
| `piece` | What was left out, when it is larger than `path`. |

Values are never listed, only locations. Provision documents (tenants, products) are listed as of the last full load, plus those read since. Registry documents are listed as of the last registry build. Users, groups and tokens are listed as of their last read. A document read again without problems leaves the list.
