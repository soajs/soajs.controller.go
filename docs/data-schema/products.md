# products

Products and their packages. A package's ACL decides which services, versions and APIs a key can reach.

- **Database:** provision. **Schema:** [products.schema.json](products.schema.json).
- **Read by the gateway:**
  - At startup: `find({})`.
  - On a cache miss or after a package's `_TTL`: `find({"packages.code": <code>})` (Node.js soajs.core/provision/mongo.js:164-169; soajs.provision/index.js:163-183).
  - The package ACL for the environment is `acl[<env>]` (mongo.js:187-202).
- **Written by** the multitenant service (model/mongo/product.js; bl/product.js).
- **Indexes the gateway relies on:** `{code: 1}` unique and `{"packages.code": 1}`. Node.js creates both (soajs.core/provision/mongo.js:56-59).

## Fields

| Path | Type | Required | Values | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | |
| `code` | string | yes | uppercase `^[A-Z0-9]+$` | Product code. |
| `name` | | | | Not used by the gateway. |
| `description` | string | | | |
| `console` | | | | Not used by the gateway. |
| `locked` | boolean | | | Not used by the gateway. |
| `scope` | object | | | Not used by the gateway: scope ACL expansion is not part of this spec. |
| `packages[].code` | string | yes | `<PRODUCT>_<CODE>`, uppercase | Package code, as in `applications[].package` and the extKey. |
| `packages[].name` | | | | Not used by the gateway. |
| `packages[].description`, `tags`, `locked` | | | | Not used by the gateway. |
| `packages[]._TTL` | integer (ms) | yes | | How long the gateway caches the package. |
| `packages[].aclTypeByEnv.<env>` | | | `granular` | Not used by the gateway. |
| `packages[].acl.<env>` | object | yes (may be empty) | lowercase env code | The package's ACL in that environment, keyed by service. |
| `….<service>.<version>` | object | | integer string version | ACL of one service version. |
| `….<version>.access` | boolean | yes | | `true`: the service version needs a logged-in user; `false`: open. |
| `….<version>.apisPermission` | string | | `restricted` | Only the APIs listed below are allowed. Absent: every API of the version is allowed. |
| `….<version>.<method>` | object | | `get`, `post`, `put`, `delete`, `patch`, `head`, `options` | A version without methods gives its `access` to every API of the version, as Node.js reads it (keyACL/index.js:96-97). |
| `….<method>.apis.<path>` | object | | the API path as in marketplace, `:param` allowed | |
| `….<path>.access` | boolean | yes | | `true`: logged-in user needed; `false`: public. |
| `….<path>.group` | string | | | The API group label. Not used for access. |

How a product that does not match is read, piece by piece: [imperfect-data.md](imperfect-data.md).

## Not part of this spec

- `aclType` (global), and packages that are not `granular`, whose ACL is derived from `scope` (soajs.core/provision/mongo.js:193-199; provision/lib.js:11-142).
- ACL not keyed by environment (mongo.js:204-208).
- Service ACL without a version level, with `apis` / `apisRegExp` directly under the service (keyACL/index.js:35-47).
- `access` as a list of groups.
- Dotted version keys such as `"1.0"`.
- `packagesPermission`.

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "code": "SHOP",
  "name": "Shop",
  "console": false,
  "scope": {"acl": {}},
  "packages": [{
    "code": "SHOP_DEFAULT",
    "name": "Default",
    "_TTL": 21600000,
    "aclTypeByEnv": {"dev": "granular"},
    "acl": {
      "dev": {
        "orders": {
          "1": {
            "access": true,
            "apisPermission": "restricted",
            "get": {"apis": {"/list": {"access": true, "group": "Orders"}}},
            "post": {"apis": {"/checkout": {"access": true, "group": "Orders"}}}
          }
        }
      }
    }
  }]
}
```
