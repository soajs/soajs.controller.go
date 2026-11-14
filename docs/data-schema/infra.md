# infra

A kubernetes account: the API address and token of a cluster.

- **Database:** provision. **Schema:** [infra.schema.json](infra.schema.json).
- **Read by the gateway** when the environment's deployer is `container.kubernetes`: `findOne({_id: <deployer.container.kubernetes.id>})` (Node.js modules/registry/mongo.js:146-160). In HA mode the gateway asks this API for the services' cluster IPs (awareness/ha.js).
- **Written by** the infra service (model/mongo/account.js).
- **Indexes:** `_id` only.

## Fields

| Path | Type | Required | Values / default | Meaning |
|---|---|---|---|---|
| `_id` | ObjectId | yes | | Referenced by `environment.deployer.container.kubernetes.id`. |
| `configuration.url` | string | yes | host, without protocol | Kubernetes API host. |
| `configuration.token` | string | yes | secret | Service-account token. Removed before the registry is passed on to services. |
| `configuration.port` | integer | | | API port. |
| `configuration.protocol` | string | | `http` \| `https`; default `https` | |
| `configuration.ca` | object | | | Optional. The cluster's TLS settings, passed to the Kubernetes client as its request options (Node.js modules/driver/kubernetes/model/index.js connect). With it, the API address and TLS come from `ca.requestOptions`, not from `url`/`port`/`protocol`. Without it, the gateway connects to `url` without verifying the server certificate. |
| `configuration.ca.requestOptions.baseUrl` | string | with `ca` | full URL | Kubernetes API address. |
| `configuration.ca.requestOptions.ca` | string \| string[] | | PEM | Certificate authority the server certificate is verified against. |
| `configuration.ca.requestOptions.cert`, `.key` | string | | PEM | Client certificate and key. |
| `configuration.ca.requestOptions.strictSSL` | boolean | | default `true` | `false` skips the server certificate check. |
| `configuration.ca.requestOptions.auth.bearer` | string | | secret | Token; default `configuration.token`. |
| `configuration.type` | string | | | Not used by the gateway. |
| `type`, `label`, `description`, `ts`, `deployments`, `settings` | | | | Not used by the gateway. |

## Example

```json
{
  "_id": {"$oid": "<id>"},
  "type": "kubernetes",
  "label": "production cluster",
  "configuration": {"type": "secret", "url": "<api host>", "port": 6443, "token": "<token>"},
  "deployments": [{"env": "dev", "namespace": "dev"}]
}
```
