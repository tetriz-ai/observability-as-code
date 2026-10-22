#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Starts the `make dev-monitoring` stack, checks that Grafana provisioned every generated
# dashboard, the community ones in external/ included, and can reach Prometheus through the
# provisioned datasource, then stops the stack. make check-dev-stack runs it.
#
#   GRAFANA_PORT, PROMETHEUS_PORT  host ports (defaults 3000, 9090), as in docker-compose.yml
#   DASHBOARDS_OUT                 the generator's output directory, as in the Makefile
#   GRAFANA_AUTH                   basic-auth user:password (default admin:admin, Grafana's
#                                  first-start login)
#
# Needs: docker compose, go, make, curl, jq.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GRAFANA="http://localhost:${GRAFANA_PORT:-3000}"
GRAFANA_AUTH=${GRAFANA_AUTH:-admin:admin}
trap 'make -s -C "$ROOT" dev-monitoring-down >/dev/null 2>&1 || true' EXIT

make -s -C "$ROOT" dev-monitoring

OUT=${DASHBOARDS_OUT:-$ROOT/dashboards/output}
shopt -s nullglob
fail=0
for f in "$OUT"/*.json "$OUT"/external/*.json; do
  uid=$(jq -r .uid "$f")
  title=""
  for _ in $(seq 1 30); do
    title=$(curl -sf -u "$GRAFANA_AUTH" "$GRAFANA/api/dashboards/uid/$uid" | jq -r '.dashboard.title // empty' || true)
    [ -n "$title" ] && break
    sleep 1
  done
  if [ -n "$title" ]; then echo "✓ provisioned $uid: \"$title\""; else echo "✗ $uid was not provisioned" >&2; fail=1; fi
done

health=$(curl -s -u "$GRAFANA_AUTH" "$GRAFANA/api/datasources/uid/prometheus/health" | jq -r '.status // empty')
if [ "$health" = "OK" ]; then echo "✓ datasource prometheus: OK"; else echo "✗ datasource prometheus health: ${health:-no answer}" >&2; fail=1; fi

[ "$fail" -eq 0 ] && echo "dev stack: PASS"
exit "$fail"
