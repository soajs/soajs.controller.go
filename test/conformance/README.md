# Conformance tests

The Go gateway must answer as the Node.js gateway (`soajs.controller`) does, byte for byte. These tests send a fixed set of requests to a running gateway and compare each answer with the one Node.js gave.

| Path | What it is |
|---|---|
| `cases.json` | The requests. |
| `golden/` | Node.js's answers: status, compared headers and body. |
| `pending.json` | Cases the Go gateway does not pass yet, each with the pull request that ports it. |
| `fixture/` | The data both gateways run on, in the shapes of the live `core_provision` data. Every value in it is a test value. |
| `cmd/fixture` | Loads the fixture. It writes only the `conformance_*` databases, and only on a local MongoDB. |
| `cmd/mockservice` | The `conform` service of the fixture. It answers with the method, path and query it received. |
| `run.sh` | Loads the fixture, starts the mock and a gateway, and runs the cases. |

## Run

A local MongoDB on `127.0.0.1:27017` is needed, or set `MONGO_URI`. Ports 24000, 25000, 24100 and 25100 must be free.

```bash
test/conformance/run.sh go
```

A pending case that fails is skipped with its reason. A pending case that passes fails the run, so remove it from `pending.json` in the pull request that fixes it.

## Record

After a case is added, or the Node.js gateway changes, record the answers from Node.js:

```bash
NODE_CONTROLLER_DIR=/path/to/soajs.controller test/conformance/run.sh node -record
test/conformance/run.sh node    # check the recording is stable
```

`NODE_CONTROLLER_DIR` is a checkout of `soajs.controller` with its `node_modules` installed. The golden files in this repository were recorded from 4.3.28.

## Adding a case

- `target`: `maintenance` for the maintenance port; the gateway port otherwise.
- `$KEY_DEFAULT`, `$KEY_NOSVC`, `$TOKEN_VALID` and `$TOKEN_EXPIRED` refer to the fixture's keys and tokens (`vars` in `conformance_test.go`).
- `compareHeaders`: headers compared besides `Content-Type`.
- `ignoreFields`: JSON fields whose value changes on every call, such as `ts`. They are compared as `null`.
