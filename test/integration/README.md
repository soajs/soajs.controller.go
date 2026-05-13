# Integration Tests

Integration tests for the Go SOAJS Controller.

## Prerequisites

1. MongoDB running on port 32017 (or as configured in SOAJS_MONGO_URI)
2. Go controller built: `go build -o controller ./cmd/controller`

## Running Tests

### Option 1: With running controller

Start the controller in one terminal:
```bash
./start_local.sh
```

Run tests in another terminal:
```bash
INTEGRATION_TEST=true go test -v ./test/integration/...
```

### Option 2: Run all tests with script

```bash
./test/integration/run_tests.sh
```

## Test Suites

### UC1: Gateway Routes (`gateway_routes_test.go`)
- `/soajs/acl` - ACL endpoint without/with key
- `/soajs/proxy` - Proxy endpoint with proxyRoute parameter
- Service routing to urac, oauth

### UC2: HTTP Methods (`http_methods_test.go`)
- GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
- Tests all HTTP methods are properly proxied

### UC3: Maintenance Endpoints (`maintenance_test.go`)
- `/heartbeat` - Health check
- `/getRegistry` - Get registry data
- `/awarenessStat` - Service awareness statistics
- `/loadProvision` - Reload provision data
- `/reloadRegistry` - Reload registry

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INTEGRATION_TEST` | Enable integration tests | false |
| `CONTROLLER_PORT` | Controller gateway port | 4000 |
| `MAINTENANCE_PORT` | Maintenance server port | 5001 |
