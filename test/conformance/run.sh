#!/usr/bin/env bash
# Runs the conformance cases against the gateways of one implementation on the fixture: one for
# the dev environment, one for the dashboard environment.
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
DASHBOARD_PORT=26000
DASHBOARD_MAINTENANCE_PORT=27000
WORK="$(mktemp -d)"
GATEWAY_PID=""
DASHBOARD_PID=""
MOCK_PID=""
OTHER_PID=""
OPEN_PID=""

# stop PID: SIGTERM, then kill -9 after 5 s. Both gateways exit on the SIGTERM itself (the Node.js
# gateway since 4.3.30); the kill -9 stays for a process that hangs.
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
	stop "$DASHBOARD_PID"
	stop "$MOCK_PID"
	stop "$OTHER_PID"
	stop "$OPEN_PID"
	rm -rf "$WORK"
}
trap cleanup EXIT

cd "$ROOT"
for port in $GATEWAY_PORT $MAINTENANCE_PORT $DASHBOARD_PORT $DASHBOARD_MAINTENANCE_PORT 24100 25100 24200 25200 24400 25400; do
	if curl -s -o /dev/null "http://127.0.0.1:$port/"; then
		echo "port $port is already in use" >&2
		exit 1
	fi
done

go run ./test/conformance/cmd/fixture -uri="$MONGO_URI"
go build -o "$WORK/mockservice" ./test/conformance/cmd/mockservice
"$WORK/mockservice" -port 24100 -maintenance 25100 -tools soajs > "$WORK/mockservice.log" 2>&1 &
MOCK_PID=$!
# the other service, for the response cache and the MCP tools of an OpenAPI document
"$WORK/mockservice" -port 24200 -maintenance 25200 -tools swagger > "$WORK/mockservice-other.log" 2>&1 &
OTHER_PID=$!
# the open service, whose version needs no key
"$WORK/mockservice" -port 24400 -maintenance 25400 > "$WORK/mockservice-open.log" 2>&1 &
OPEN_PID=$!

MONGO_HOSTPORT="${MONGO_URI#mongodb://}"
MONGO_HOSTPORT="${MONGO_HOSTPORT%%/*}"
MONGO_HOST="${MONGO_HOSTPORT%%:*}"
MONGO_PORT="${MONGO_HOSTPORT##*:}"

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

# start ENV PORT LOG: starts a gateway of the environment, its PID in STARTED
start() {
	if [ "$IMPL" = "node" ]; then
		: "${NODE_CONTROLLER_DIR:?set NODE_CONTROLLER_DIR to a soajs.controller checkout}"
		(cd "$NODE_CONTROLLER_DIR" && \
			SOAJS_ENV=$1 SOAJS_PROFILE="$WORK/profile.js" SOAJS_SRVIP=127.0.0.1 SOAJS_DEPLOY_MANUAL=1 \
			NODE_ENV=production exec node index.js) > "$3" 2>&1 &
	else
		[ -x "$WORK/controller" ] || go build -o "$WORK/controller" ./cmd/controller
		SOAJS_ENV=$1 SOAJS_PROFILE="$WORK/profile.js" SOAJS_SRVIP=127.0.0.1 \
			SOAJS_SERVICE_PORT=$2 SOAJS_MAINTENANCE_PORT_INCREMENT=1000 SOAJS_LOG_LEVEL=error \
			"$WORK/controller" > "$3" 2>&1 &
	fi
	STARTED=$!
}

# wait_for PID MAINTENANCE_PORT LOG
wait_for() {
	for i in $(seq 1 60); do
		if curl -sf "http://127.0.0.1:$2/heartbeat" > /dev/null 2>&1; then
			return 0
		fi
		if ! kill -0 "$1" 2>/dev/null || [ "$i" = 60 ]; then
			echo "the $IMPL gateway did not start:" >&2
			cat "$3" >&2
			exit 1
		fi
		sleep 1
	done
}

start dev $GATEWAY_PORT "$WORK/gateway.log"
GATEWAY_PID=$STARTED
start dashboard $DASHBOARD_PORT "$WORK/dashboard.log"
DASHBOARD_PID=$STARTED
wait_for "$GATEWAY_PID" $MAINTENANCE_PORT "$WORK/gateway.log"
wait_for "$DASHBOARD_PID" $DASHBOARD_MAINTENANCE_PORT "$WORK/dashboard.log"

CONFORMANCE_IMPL="$IMPL" \
CONFORMANCE_GATEWAY="http://127.0.0.1:$GATEWAY_PORT" \
CONFORMANCE_MAINTENANCE="http://127.0.0.1:$MAINTENANCE_PORT" \
CONFORMANCE_DASHBOARD_GATEWAY="http://127.0.0.1:$DASHBOARD_PORT" \
CONFORMANCE_DASHBOARD_MAINTENANCE="http://127.0.0.1:$DASHBOARD_MAINTENANCE_PORT" \
CONFORMANCE_MONGO_URI="$MONGO_URI" \
	go test -count=1 -v ./test/conformance/ -run TestConformance -args "$@"
