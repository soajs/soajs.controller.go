# Conformance tests

The Go gateway must answer as the Node.js gateway (`soajs.controller`) does, byte for byte. These tests send a fixed set of requests to a running gateway and compare each answer with the one Node.js gave.

| Path | What it is |
|---|---|
| `cases.json` | The requests. |
| `golden/` | Node.js's answers: status, compared headers and body. |
| `pending.json` | Cases the Go gateway does not pass yet, each with the pull request that ports it. It is empty: Go passes every case. |
| `fixture/` | The data both gateways run on, in the shapes of the live `core_provision` data. Every value in it is a test value. |
| `cmd/fixture` | Loads the fixture. It writes only the `conformance_*` databases, and only on a local MongoDB. |
| `cmd/mockservice` | The fixture's services: `conform` (24100, maintenance 25100), `other` (24200, maintenance 25200) and `open`, whose version needs no key (24400, maintenance 25400). They answer with the method, path, query and body they received and the `soajsinjectobj` the gateway sent; their maintenance port serves the MCP tools (`-tools soajs`, `-tools swagger`). |
| `run.sh` | Loads the fixture, starts the mocks and the gateways (dev on 24000/25000, dashboard on 26000/27000), and runs the cases. |

## Run

A local MongoDB on `127.0.0.1:27017` is needed, or set `MONGO_URI`. Ports 24000, 25000, 26000, 27000, 24100, 25100, 24200, 25200, 24400 and 25400 must be free.

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

`NODE_CONTROLLER_DIR` is a checkout of `soajs.controller` with its `node_modules` installed. The golden files in this repository were recorded from 4.3.29.

## Adding a case

- `target`: `maintenance` for the maintenance port; the gateway port otherwise.
- `env`: `dashboard` for the dashboard gateway; dev otherwise.
- `$KEY_DEFAULT`, `$TOKEN_VALID`, `$TOKEN_AGENT_KEY` and the others refer to the fixture's keys and tokens (`vars` in `conformance_test.go`). They are replaced in any order, so no name may be the start of another.
- `compareHeaders`: headers compared besides `Content-Type`.
- `ignoreFields`: JSON fields whose value changes on every call, such as `ts`. They are compared as `null`.
- `compareJSON`: compare the bodies as JSON values, for answers whose key order follows a map.
- `dropFields`: keys removed, at any depth, before a JSON compare.
- `sortArrays`: arrays of strings sorted before a JSON compare.

After recording, keep only the golden files whose change is real: answers that carry a timestamp change on every recording.
