# call_events

One document per call through the gateway, written only when the custom registry `gateway.events` is enabled with the `mongo` sink. The collection is a time-series collection (`timeField: ts`, `metaField: tenant`, granularity seconds) in the core database that `gateway.events.mongo.db` names. The gateway creates it when it does not exist.

- **Schema:** [call_events.schema.json](call_events.schema.json)
- **Written by:** the gateway only, in batches (up to 200 events, at least every second), off the request path. On a graceful stop the events still queued are written. When the queue is full, or the sink fails, events are dropped and the count is logged.
- **Calls covered:** a request refused from the key step on, and every request answered by the route handler. Requests refused earlier (unknown service, banned IP, maintenance mode, the MCP 401) have no event. Refusals at the key step itself (an unknown key, 148, with no tenant yet) are kept: under misuse they are the most numerous events, so size `queueSize` and the collection's retention with them in mind.

The documents carry identity and routing, never request or response bodies, query values, headers, tokens or keys. A field the call does not have is left out, never written as `null`.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `ts` | date | yes | When the call started. |
| `durationMs` | integer | yes | How long the gateway took to answer. |
| `requestId` | string | | The gateway's request id. |
| `env` | string | yes | The environment code, lowercase. |
| `tenant` | object | | `{id, code}` of the key's tenant, with `main: {id, code}` for a sub-tenant. |
| `product`, `package`, `appId` | string | | From the key. |
| `user` | object | | `{id, username}` of the signed-in user; `agent: true` when the token is restricted (`restrictedTo`). |
| `service`, `version` | string | | The service and its version. `/soajs/acl`, `/soajs/mcp` and `/soajs/proxy` are the gateway's own service. |
| `method` | string | yes | The HTTP method. |
| `api` | string | | The API route the package ACL matched (`/orders/:id`), never the raw path; for the gateway's routes, `/soajs/acl`, `/soajs/mcp` or `/soajs/proxy`. Left out when no route matched. |
| `mcpTool` | string | | The tool of a `/soajs/mcp` `tools/call`. The tool's own API call goes back through the gateway and has its own event. |
| `status` | integer | yes | The HTTP status. |
| `code` | integer | | The SOAJS error code the gateway answered, if any. |
| `idempotency` | string | | `replay` when the answer was a recorded `Idempotency-Key` answer. |
| `cache` | string | | `HIT` when the answer came from the response cache. |
| `ip` | string | | The client IP. |

No index besides the time-series bucketing; retention is the collection's to set (`expireAfterSeconds`), not the gateway's.
