#!/bin/bash

# Integration test runner for Go SOAJS Controller
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Configuration
CONTROLLER_PORT=${CONTROLLER_PORT:-4000}
MAINTENANCE_PORT=${MAINTENANCE_PORT:-5001}
MONGO_URI=${SOAJS_MONGO_URI:-"mongodb://127.0.0.1:27017"}
MONGO_DB=${SOAJS_MONGO_DATABASE:-"core_provision"}

echo "=== SOAJS Controller Integration Tests ==="
echo "Project root: $PROJECT_ROOT"
echo "Controller port: $CONTROLLER_PORT"
echo "Maintenance port: $MAINTENANCE_PORT"
echo "MongoDB URI: $MONGO_URI"
echo "MongoDB Database: $MONGO_DB"
echo ""

# Import test data into MongoDB
echo "Importing test data into MongoDB..."
cd "$PROJECT_ROOT"
go run ./test/data/cmd/import.go -uri="$MONGO_URI" -db="$MONGO_DB" || {
    echo "WARNING: Failed to import test data. Tests may fail."
}
echo ""

# Check if controller is already running
if curl -s "http://127.0.0.1:$MAINTENANCE_PORT/heartbeat" > /dev/null 2>&1; then
    echo "Controller is already running on port $CONTROLLER_PORT"
    CONTROLLER_STARTED=false
else
    echo "Starting controller..."
    cd "$PROJECT_ROOT"

    # Build if needed
    if [ ! -f "./controller" ]; then
        echo "Building controller..."
        go build -o controller ./cmd/controller
    fi

    # Start controller in background
    SOAJS_ENV=dashboard SOAJS_MONGO_URI="$MONGO_URI" ./controller > /tmp/go-ctrl-test.log 2>&1 &
    CONTROLLER_PID=$!
    CONTROLLER_STARTED=true

    echo "Controller started with PID $CONTROLLER_PID"

    # Wait for controller to be ready
    echo "Waiting for controller to be ready..."
    for i in {1..30}; do
        if curl -s "http://127.0.0.1:$MAINTENANCE_PORT/heartbeat" > /dev/null 2>&1; then
            echo "Controller is ready!"
            break
        fi
        if [ $i -eq 30 ]; then
            echo "ERROR: Controller failed to start"
            cat /tmp/go-ctrl-test.log
            exit 1
        fi
        sleep 1
    done
fi

echo ""
echo "=== Running Integration Tests ==="
echo ""

# Run tests
cd "$PROJECT_ROOT"
INTEGRATION_TEST=true go test -v ./test/integration/... 2>&1 | tee /tmp/integration-test-results.log
TEST_EXIT_CODE=${PIPESTATUS[0]}

echo ""
echo "=== Test Results ==="

# Cleanup
if [ "$CONTROLLER_STARTED" = true ]; then
    echo "Stopping controller (PID $CONTROLLER_PID)..."
    kill $CONTROLLER_PID 2>/dev/null || true
    wait $CONTROLLER_PID 2>/dev/null || true
fi

# Summary
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "All tests passed!"
else
    echo "Some tests failed. See /tmp/integration-test-results.log for details."
fi

exit $TEST_EXIT_CODE
