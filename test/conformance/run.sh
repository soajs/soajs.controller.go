#!/usr/bin/env bash
# Runs the conformance cases against one gateway on the fixture.
#
#   test/conformance/run.sh go              run the cases against the Go gateway
#   test/conformance/run.sh node            run the cases against the Node.js gateway
#   test/conformance/run.sh node -record    record golden/ from the Node.js gateway
#
# MONGO_URI                 a local MongoDB (default mongodb://127.0.0.1:27017); the fixture
#                           writes only the conformance_* databases
# NODE_CONTROLLER_DIR       a checkout of soajs.controller with its node_modules (node only)
set -euo pipefail

IMPL="${1:-}"
shift || true
if [ "$IMPL" != "go" ] && [ "$IMPL" != "node" ]; then
	echo "usage: $0 go|node [-record]" >&2
	exit 2
fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MONGO_URI="${MONGO_URI:-mongodb://127.0.0.1:27017}"
GATEWAY_PORT=24000
MAINTENANCE_PORT=25000
WORK="$(mktemp -d)"
GATEWAY_PID=""
MOCK_PID=""

stop() {
	[ -n "$1" ] || return 0
	kill "$1" 2>/dev/null || true
	for _ in 1 2 3 4 5; do
		kill -0 "$1" 2>/dev/null || return 0
		sleep 1
	done
	kill -9 "$1" 2>/dev/null || true
}

cleanup() {
	stop "$GATEWAY_PID"
	stop "$MOCK_PID"
	rm -rf "$WORK"
}
trap cleanup EXIT

cd "$ROOT"
for port in $GATEWAY_PORT $MAINTENANCE_PORT 24100 25100; do
	if curl -s -o /dev/null "http://127.0.0.1:$port/"; then
		echo "port $port is already in use" >&2
		exit 1
	fi
done

go run ./test/conformance/cmd/fixture -uri="$MONGO_URI"
go build -o "$WORK/mockservice" ./test/conformance/cmd/mockservice
"$WORK/mockservice" -port 24100 -maintenance 25100 > "$WORK/mockservice.log" 2>&1 &
MOCK_PID=$!

MONGO_HOSTPORT="${MONGO_URI#mongodb://}"
MONGO_HOSTPORT="${MONGO_HOSTPORT%%/*}"
MONGO_HOST="${MONGO_HOSTPORT%%:*}"
MONGO_PORT="${MONGO_HOSTPORT##*:}"

if [ "$IMPL" = "node" ]; then
	: "${NODE_CONTROLLER_DIR:?set NODE_CONTROLLER_DIR to a soajs.controller checkout}"
	cat > "$WORK/profile.js" <<EOF
module.exports = {
	"name": "core_provision",
	"prefix": "conformance_",
	"servers": [{"host": "$MONGO_HOST", "port": $MONGO_PORT}],
	"credentials": null,
	"streaming": {},
	"extraParam": {},
	"URLParam": {}
};
EOF
	(cd "$NODE_CONTROLLER_DIR" && \
		SOAJS_ENV=dev SOAJS_PROFILE="$WORK/profile.js" SOAJS_SRVIP=127.0.0.1 SOAJS_DEPLOY_MANUAL=1 \
		NODE_ENV=production exec node index.js) > "$WORK/gateway.log" 2>&1 &
	GATEWAY_PID=$!
else
	go build -o "$WORK/controller" ./cmd/controller
	SOAJS_ENV=dev SOAJS_MONGO_URI="$MONGO_URI" SOAJS_MONGO_DATABASE=conformance_core_provision \
		SOAJS_SERVICE_PORT=$GATEWAY_PORT SOAJS_MAINTENANCE_PORT_INCREMENT=1000 SOAJS_LOG_LEVEL=error \
		"$WORK/controller" > "$WORK/gateway.log" 2>&1 &
	GATEWAY_PID=$!
fi

for i in $(seq 1 60); do
	if curl -sf "http://127.0.0.1:$MAINTENANCE_PORT/heartbeat" > /dev/null 2>&1; then
		break
	fi
	if ! kill -0 "$GATEWAY_PID" 2>/dev/null || [ "$i" = 60 ]; then
		echo "the $IMPL gateway did not start:" >&2
		cat "$WORK/gateway.log" >&2
		exit 1
	fi
	sleep 1
done

CONFORMANCE_IMPL="$IMPL" \
CONFORMANCE_GATEWAY="http://127.0.0.1:$GATEWAY_PORT" \
CONFORMANCE_MAINTENANCE="http://127.0.0.1:$MAINTENANCE_PORT" \
	go test -count=1 -v ./test/conformance/ -run TestConformance -args "$@"
