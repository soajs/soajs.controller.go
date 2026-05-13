# Integration Tests

Integration tests for the Go SOAJS Controller.

## Test Summary

| Suite | Tests | Status |
|-------|-------|--------|
| Gateway Routes | 18 | ✅ Pass |
| HTTP Methods | 7 | ✅ Pass |
| Maintenance Endpoints | 9 | ✅ Pass |
| Roaming | 3 | ✅ Pass |
| **Total** | **37** | **✅ All Pass** |

## Prerequisites

1. MongoDB running (default: port 32017)
2. Go controller built: `go build -o controller ./cmd/controller`

## Running Tests

### Quick Start

```bash
# Run all integration tests (starts controller if needed)
cd test/integration && ./run_tests.sh
```

### Manual Run

Start the controller in one terminal:
```bash
./start_local.sh
```

Run tests in another terminal:
```bash
INTEGRATION_TEST=true go test -v ./test/integration/...
```

## Mock Services

The integration tests automatically start mock backend services:

| Service | Service Port | Maintenance Port |
|---------|--------------|------------------|
| urac    | 4001         | 5002             |
| oauth   | 4004         | 5005             |
| pt      | 4002         | 5003             |
| pt2     | 4003         | 5004             |

Mock services provide endpoints that match the Node.js test infrastructure:
- **urac**: `/user`, `/luser`, `/muser/:id`, `/private`
- **oauth**: `/passport/login`, `/token`
- **pt/pt2**: `/CheckPhoneNumber` and catch-all endpoints

## Test Suites

### Gateway Routes (`gateway_routes_test.go`)

| Test | Description |
|------|-------------|
| SoajsACL/NoKey | ACL endpoint without API key (should fail) |
| SoajsACL/WithKey | ACL endpoint with valid API key |
| SoajsProxy/MissingProxyRoute | Proxy without proxyRoute param (should fail) |
| SoajsProxy/ValidProxyRoute | Proxy with valid proxyRoute to urac |
| SoajsProxy/FullURI | Proxy with full URI in proxyRoute |
| UracUser | Route to urac /user endpoint |
| UracMuser | Route to urac /muser/:id endpoint |
| OAuthPassport | Route to oauth /passport/login |
| PTEndpoint | Route to passthrough service |

### HTTP Methods (`http_methods_test.go`)

Tests all HTTP methods are properly proxied through the gateway:
- GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS

### Maintenance Endpoints (`maintenance_test.go`)

| Endpoint | Description |
|----------|-------------|
| /heartbeat | Health check |
| /getRegistry | Get registry data for a service |
| /awarenessStat | Service awareness statistics |
| /loadProvision | Reload provision data |
| /reloadRegistry | Reload registry |
| /register | Register a service host |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INTEGRATION_TEST` | Enable integration tests | false |
| `SOAJS_MONGO_URI` | MongoDB connection URI | mongodb://127.0.0.1:27017 |
| `CONTROLLER_PORT` | Controller gateway port | 4000 |
| `MAINTENANCE_PORT` | Maintenance server port | 5001 |

## Test Data

Tests use the following constants (matching Node.js tests):
- **extKey**: External API key for authentication
- **validAccessToken**: Mock OAuth token (triggers validation error without DB token)
