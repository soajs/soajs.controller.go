# tenants

Tenants, their applications and keys, and their oauth settings.

- **Database:** provision. **Schema:** [tenants.schema.json](tenants.schema.json).
- **Read by the gateway** (Node.js soajs.provision/index.js; soajs.core/provision/mongo.js:240-346):
  - `find({})` at startup and on `/loadProvision`;
  - `find({"applications.keys.key": <iKey>})` on a key cache miss or after the application's `_TTL`;
  - `find({_id: <id>})` for the tenant and its oauth;
  - `findOne({code: <CODE>})` for the cross-env proxy.
- **Written by** the multitenant service (model/mongo/tenant.js; bl/tenant.js).
- **Indexes the gateway relies on:** `{code: 1}` unique and `{"applications.keys.key": 1}`. Node.js creates both (soajs.core/provision/mongo.js:52-55).

## Fields

| Path | Type | Required | Values / default | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | `tenant.id` (as a string) everywhere else. |
| `code` | string | yes | uppercase `^[A-Z0-9]+$` | Tenant code. |
| `name` | string | | | Injected. |
| `description`, `tag`, `category` | string | | | Not used by the gateway. |
| `type` | string | yes | `product` \| `client` | `client` is a sub-tenant. |
| `console` | | | | Not used by the gateway. |
| `locked` | boolean | | | Injected only. |
| `tenant.id`, `tenant.code` | string | for `client` | the main tenant | Injected as `tenant.main` for a `client`. |
| `profile` | object | | free-form | Injected when the service version has `tenant_Profile`. |
| `profile.access.resolver` | string | | a name in `custom_registry` `urac` `tenantAccessResolvers` | Cross-tenant access resolver. |
| `profile.access.subject` | string | | | |
| `oauth.secret` | | | secret | Not used by the gateway (tokens are issued by oauth). |
| `oauth.type` | | | `2` | Tokens are stored in `oauth_token`. Type 0 (JWT) is not part of this spec (D5). |
| `oauth.disabled` | integer | | `0` \| `1`; missing is `0` | `1`: no user login is required for the tenant's private APIs. |
| `oauth.pin.<product>.enabled` | boolean | | | Requires the user's PIN under that product. |
| `oauth.loginMode`, `grants`, `redirectURI` | | | | Not used by the gateway. |
| `applications[].appId` | ObjectId | yes | | Injected. |
| `applications[].product` | string | yes | product code | |
| `applications[].package` | string | yes | `<PRODUCT>_<CODE>` | The application's package. |
| `applications[].description` | string | | | Not used by the gateway. |
| `applications[]._TTL` | integer (ms) | yes | | How long the gateway caches a key. |
| `applications[].keys[].key` | string | yes | 32 hex characters | Internal key (inside the extKey). |
| `applications[].keys[].config.<env>` | object | yes (map may be empty) | lowercase env code | The key's configuration in that environment, keyed by service. Injected as `key.config`. |
| `….config.<env>.commonFields` | object | | | Merged into every service's config. |
| `….config.<env>.gateway.networkPackages.<network>.<product>` | object | | `default`, `users.<package>` | Package override for a network. |
| `….config.<env>.gateway.validateTokenAccount.email`, `phone` | boolean | | | Refuses a token whose email or phone no longer matches the user (146). |
| `….config.<env>.gateway.throttling.<service>` | object | | as in custom_registry `traffic.throttling` | Tenant-level strategy. Wins over the custom registry. |
| `applications[].keys[].extKeys[].extKey` | string | yes | hex | The external key clients send. |
| `….extKeys[].env` | string | yes | uppercase env code | Only valid in that environment (144). |
| `….extKeys[].expDate` | integer (ms) \| null | | `null` or missing: never expires | |
| `….extKeys[].device` | object \| null | | `null` or `{}`: no restriction; otherwise `allow` and/or `deny` | Device restriction (156). |
| `….extKeys[].geo` | object \| null | | `null` or `{}`: no restriction; otherwise `allow` and/or `deny` CIDR lists | IP restriction (155). |
| `….extKeys[].label` | string | | | Not used by the gateway. |
| `….extKeys[].dashboardAccess` | boolean | | | Not used by the gateway. |

How a tenant that does not match is read, piece by piece: [imperfect-data.md](imperfect-data.md).

## Not part of this spec

- `applications[].acl`, `acl_all_env`, and ACL not keyed by environment (soajs.core/provision/mongo.js:287-302).
- `oauth.type` 0 (JWT).
- `expDate` as a date string; the schema refuses it.

## Example

```json
{
  "_id": {"$oid": "<tenant id>"},
  "code": "ACME",
  "name": "Acme",
  "type": "product",
  "console": false,
  "profile": {},
  "oauth": {"secret": "<secret>", "type": 2, "disabled": 0, "loginMode": "urac", "grants": ["password", "refresh_token"], "redirectURI": ""},
  "applications": [{
    "appId": {"$oid": "<app id>"},
    "product": "SHOP",
    "package": "SHOP_DEFAULT",
    "description": "Web app",
    "_TTL": 604800000,
    "keys": [{
      "key": "<32 hex>",
      "config": {"dev": {"orders": {"currency": "USD"}}},
      "extKeys": [{"extKey": "<extKey>", "env": "DEV", "label": "web", "expDate": null, "device": null, "geo": null}]
    }]
  }]
}
```
