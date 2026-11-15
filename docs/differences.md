# Differences from the Node.js gateway

The Go gateway answers as soajs.controller 4.3.30 does, and the [conformance harness](../test/conformance/README.md) checks it. This page lists where it deliberately does not, each one reviewed in the pull request given.

## Not ported

| | |
|---|---|
| oauth type 0 (JWT access tokens) | #10 |
| Endpoint-type services (`src.url(s)`, a key's `config.url`, apibuilder) | #7 |
| `/proxy/redirect`, the old name of `/soajs/proxy`; `/proxySocket/*` on the maintenance port | #7, #17 |
| `/register` by GET, and `?name=` on `/getRegistry` (`?serviceName=`) | #16 |
| Older data shapes: application ACLs, flat `apis`/`apisRegExp`, ACLs without a version, `access` as a list of groups (157, 160), `dbs.clusters`, `custom` inside the environment, the old deployer, hosts without `version`, per-tenant urac databases (`masterDB` only), `maintenance.port.value` | #5-#8, #11, #20 |

Go reads only the latest data schema ([data-schema](data-schema/README.md)). A document that does not match it is logged and skipped or refused.

## Registry and maintenance port

- **Version:** the maintenance answers and MCP's `serverInfo.version` give the Go gateway's own version.
- **Pool options:** registry dumps never include `maxPoolSize`, `wtimeoutMS`, `slaveOk`, `auto_reconnect` or `extraParam`.
- **Throttling model:** throttling does not record its model on the registry, so `/getRegistry` does not show it.
- **`/register` with `mw`:** a field the registration did not give is not written to the marketplace (Node.js writes `null`).
- **`/register` timeouts:** a new service without `requestTimeout`/`requestTimeoutRenewal` gets 0.
- **Invalid input:** an unparsable maintenance URL or `/register` body is answered: false, or the heartbeat.
- **mdaemon `/getRegistry`:** `?type=mdaemon` answers the daemon.
- **Key order:** JSON key order in registry dumps can differ.

## Awareness

- **HA namespace:** a deployer without a namespace gives no host; Node.js gives the host name without a namespace. (Go reads only the current deployer shape; see Not ported.)
- **Stopping `controllers` document:** written with `daemons: {}`; Node.js leaves `daemons` out.

## Proxy and interConnect

- **Request timeout renewals:** each renewal counts once.
- **interConnect:** when awareness has no gateway host, the awareness block and the interConnect peers are left out.

## urac

- **Resolver timeout:** the access resolver call has a total timeout.
- **Membership ties:** a tie between two main products is broken by name.
- **User ACL version:** a requested version absent from the user's ACL answers 154.

## Gateway routes

- **`/soajs/acl`:** the finalACL API lists are sorted.
- **`/soajs/proxy`:** an invalid `%` escape in `proxyRoute` answers 139.
- **`/soajs/mcp`:**
  - a tool call that fails on the network returns Go's error text;
  - an `operationId` that is not a string is not used in the tool name.

## Idempotency and cache

- **Mongo idempotency lock:** taken in one atomic operation, so two requests cannot both lock a key.
- **Client gone mid-answer:** the service's full answer is still recorded for idempotency, and stored in the cache.
- **Replay:** a recorded or cached answer is replayed without its hop-by-hop headers.
- **Cache API match:** when several `:param` entries match a path, the first in sorted order wins. An exact entry always wins.
