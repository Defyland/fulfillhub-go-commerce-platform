#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

RESULT_ROOT=${RESULT_ROOT:-benchmarks/results}
STAMP=${STAMP:-$(date -u +%Y-%m-%dT%H-%M-%SZ)}
RESULT_DIR="$RESULT_ROOT/compose-$STAMP"
API_PORT=${API_PORT:-8080}
RABBITMQ_MANAGEMENT_PORT=${RABBITMQ_MANAGEMENT_PORT:-15672}
BASE_URL=${BASE_URL:-http://localhost:${API_PORT}}
SCENARIOS=${SCENARIOS:-"smoke load stress spike"}
KEEP_STACK=${KEEP_STACK:-0}
DRAIN_TIMEOUT_SECONDS=${DRAIN_TIMEOUT_SECONDS:-60}
RATE_LIMIT_PER_MINUTE=${RATE_LIMIT_PER_MINUTE:-60000}
export RATE_LIMIT_PER_MINUTE

mkdir -p "$RESULT_DIR"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

capture() {
  name=$1
  shift
  output="$RESULT_DIR/$name"
  if "$@" >"$output" 2>&1; then
    return 0
  fi
  status=$?
  {
    echo
    echo "command failed with status $status"
  } >>"$output"
  return 0
}

capture_shell() {
  name=$1
  command=$2
  output="$RESULT_DIR/$name"
  if sh -c "$command" >"$output" 2>&1; then
    return 0
  fi
  status=$?
  {
    echo
    echo "command failed with status $status"
  } >>"$output"
  return 0
}

wait_for_api() {
  deadline=$(($(date +%s) + 90))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "API did not become ready at $BASE_URL/readyz" >&2
  exit 1
}

wait_for_async_drain() {
	label=$1
	deadline=$(($(date +%s) + DRAIN_TIMEOUT_SECONDS))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		metrics=$(curl -fsS "$BASE_URL/metrics" 2>/dev/null || true)
		outbox=$(printf '%s\n' "$metrics" | awk '$1 == "fulfillhub_outbox_unpublished_total" { print int($2); found = 1 } END { if (!found) print -1 }')
		queue_state=$(curl -fsS --max-time 5 -u guest:guest "http://localhost:${RABBITMQ_MANAGEMENT_PORT}/api/queues" 2>/dev/null | python3 -c 'import json, sys; queues = json.load(sys.stdin); print(sum(q.get("messages_ready", 0) for q in queues), sum(q.get("messages_unacknowledged", 0) for q in queues))' 2>/dev/null || true)
		if [ -z "$queue_state" ]; then
			queued=-1
			unacked=-1
		else
			queued=$(printf '%s\n' "$queue_state" | awk 'NF >= 2 { print int($1); found = 1 } END { if (!found) print -1 }')
			unacked=$(printf '%s\n' "$queue_state" | awk 'NF >= 2 { print int($2); found = 1 } END { if (!found) print -1 }')
		fi
		if [ "$outbox" -eq 0 ] && [ "$queued" -eq 0 ] && [ "$unacked" -eq 0 ]; then
			return 0
		fi
		sleep 2
	done
	echo "async drain did not complete for $label after ${DRAIN_TIMEOUT_SECONDS}s" >&2
	echo "last outbox backlog: $outbox; last queued RabbitMQ messages: $queued; last unacknowledged RabbitMQ messages: $unacked" >&2
	exit 1
}

snapshot() {
  label=$1
  capture "compose-ps-$label.txt" docker compose ps
  capture_shell "docker-stats-$label.txt" 'containers=$(docker compose ps -q); if [ -n "$containers" ]; then docker stats --no-stream $containers; else echo "no compose containers"; fi'
  capture "api-metrics-$label.prom" curl -fsS "$BASE_URL/metrics"
  capture "rabbitmq-queues-$label.json" curl -fsS -u guest:guest "http://localhost:${RABBITMQ_MANAGEMENT_PORT}/api/queues"
  capture "redis-memory-$label.txt" docker compose exec -T redis redis-cli INFO memory
  capture "postgres-activity-$label.txt" docker compose exec -T postgres psql -U fulfillhub -d fulfillhub -c "SELECT datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit FROM pg_stat_database WHERE datname = 'fulfillhub';"
}

require_command docker
require_command curl
require_command k6
require_command python3

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not available; start Docker before running compose profiling." >&2
  exit 1
fi

if [ "$KEEP_STACK" != "1" ]; then
  trap 'docker compose down >/dev/null 2>&1 || true' EXIT INT TERM
fi

capture "docker-version.txt" docker version
capture "compose-config.yml" docker compose config

docker compose up -d --build
wait_for_api

snapshot "before"

for scenario in $SCENARIOS; do
  script="benchmarks/k6/$scenario.js"
  if [ ! -f "$script" ]; then
    echo "missing k6 scenario: $script" >&2
    exit 1
  fi
  snapshot "before-$scenario"
  summary="$RESULT_DIR/k6-$scenario-summary.json"
  log="$RESULT_DIR/k6-$scenario.log"
  if ! K6_SUMMARY_EXPORT="$summary" BASE_URL="$BASE_URL" k6 run "$script" >"$log" 2>&1; then
    echo "k6 scenario failed: $scenario; see $log" >&2
    exit 1
  fi
  wait_for_async_drain "$scenario"
  snapshot "after-$scenario"
done

snapshot "after"

cat >"$RESULT_DIR/README.md" <<EOF
# Compose Profiling Run

- Timestamp: $STAMP
- Base URL: \`$BASE_URL\`
- Scenarios: $SCENARIOS
- Async drain timeout: ${DRAIN_TIMEOUT_SECONDS}s
- Result directory: $RESULT_DIR

Captured artifacts include Docker stats, API Prometheus metrics, RabbitMQ queue
state, Redis memory info, PostgreSQL activity, k6 logs, k6 summary exports, and
post-scenario snapshots taken only after unpublished outbox, ready queue, and
unacknowledged queue metrics drain to zero.
EOF

echo "compose profiling artifacts written to $RESULT_DIR"
