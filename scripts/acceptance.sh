#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Acceptance test: generate every dashboard, lint it, import it into a blank Grafana and read it
# back, lint and unit-test the alert rules, and check every metric name the generated dashboards,
# the community ones in external/ included, and the rule files read against ALLOWED below.
# ALLOWED lists the example dashboards' and alerts' metrics; a metric of your own goes into it
# when you add a dashboard or rule that reads it, and so do a fetched community dashboard's.
# make acceptance runs it.
#
#   GRAFANA_URL   a Grafana with no dashboards (default http://localhost:3001, the compose
#                 service grafana-blank); `make acceptance` starts one
#   GRAFANA_AUTH  basic-auth user:password (default admin:admin, Grafana's first-start login)
#
# `scripts/acceptance.sh --check-names <dir>` runs only the metric-name check, on an output
# directory the generator wrote, with no Grafana; scripts/test-metric-names.sh uses it.
#
# Needs: go, make, curl, jq, yq, promtool, dashboard-linter (jq and yq for --check-names).

set -euo pipefail

GRAFANA_URL=${GRAFANA_URL:-http://localhost:3001}
GRAFANA_AUTH=${GRAFANA_AUTH:-admin:admin}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Resolved with pwd -P: the generator refuses an output path reached through a symbolic link, and
# the temporary directory on macOS is under one, /var.
WORK=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$WORK"' EXIT
OUT="$WORK/output"

# Every metric name the example dashboards and alerts read: asynq's x/metrics collector, the
# go-redis-prometheus hook, kube-state-metrics, cAdvisor (scraped from the kubelet),
# go-grpc-prometheus, and Prometheus's own ALERTS, which AsynqQueueFailuresLowVolume reads.
ALLOWED='asynq_(tasks_enqueued_total|tasks_processed_total|tasks_failed_total|queue_size|queue_latency_seconds|queue_memory_usage_approx_bytes|queue_paused_total)|redis_(single_commands_(bucket|count)|single_errors|pipelined_commands|pipelined_errors)|kube_pod_container_(status_restarts_total|status_last_terminated_reason|resource_limits)|container_memory_working_set_bytes|grpc_server_handling_seconds_(bucket|count)|ALERTS'

step() { printf '\n== %s\n' "$1"; }

# check_names <output dir>: every metric name the dashboards in <dir> and <dir>/external/ and the
# rule files read is listed in ALLOWED.
check_names() {
  local out=$1 dashboards
  shopt -s nullglob
  dashboards=("$out"/*.json "$out"/external/*.json)
  shopt -u nullglob
  # scripts/metric-names.sh reads the metric names out of every expression, and refuses one that
  # names its metric inside a label matcher, where this check could not see it.
  {
    if [ "${#dashboards[@]}" -gt 0 ]; then jq -r '.. | objects | .expr? // empty | gsub("\n"; " ")' "${dashboards[@]}"; fi
    for f in "$ROOT"/alerts/rules*.y*ml; do yq -o=json '[.spec.groups[].rules[].expr]' "$f" | jq -r '.[] | gsub("\n"; " ")'; done
  } > "$WORK/exprs"
  bash "$ROOT/scripts/metric-names.sh" < "$WORK/exprs" > "$WORK/names" ||
    { echo "✗ a metric is named inside a label matcher, so ALLOWED cannot check it" >&2; exit 1; }
  if [ "${#dashboards[@]}" -gt 0 ]; then
    jq -r '.templating.list[]? | select(.type == "query") | .query | strings' "${dashboards[@]}" |
      sed -nE 's/^label_values\(([^,)]+).*/\1/p' | tr -d ' ' >> "$WORK/names" || true
  fi
  sort -u "$WORK/names" -o "$WORK/names"
  # An extraction that found nothing would pass the check below vacuously.
  for must in asynq_tasks_enqueued_total redis_single_commands_bucket kube_pod_container_status_restarts_total grpc_server_handling_seconds_bucket; do
    grep -qx "$must" "$WORK/names" || { echo "✗ metric extraction missed $must; the check below would prove nothing" >&2; exit 1; }
  done
  unknown=$(grep -vxE "$ALLOWED" "$WORK/names" || true)
  if [ -n "$unknown" ]; then
    echo "✗ metric names missing from ALLOWED in scripts/acceptance.sh:" >&2
    echo "$unknown" >&2
    exit 1
  fi
  echo "✓ $(wc -l < "$WORK/names" | tr -d ' ') metric names, all in ALLOWED: $(paste -sd' ' "$WORK/names")"
}

if [ "${1:-}" = --check-names ]; then
  [ -d "${2:-}" ] || { echo "usage: scripts/acceptance.sh --check-names <output dir>" >&2; exit 2; }
  for bin in jq yq; do
    command -v "$bin" >/dev/null 2>&1 || { echo "missing required binary: $bin" >&2; exit 2; }
  done
  check_names "$2"
  exit 0
fi

for bin in go make curl jq yq promtool dashboard-linter; do
  command -v "$bin" >/dev/null 2>&1 || { echo "missing required binary: $bin" >&2; exit 2; }
done

step "generate and lint dashboards"
make -s -C "$ROOT" lint-dashboards DASHBOARDS_OUT="$OUT"

step "import into $GRAFANA_URL"
for _ in $(seq 1 60); do
  curl -sf "$GRAFANA_URL/api/health" >/dev/null && break
  sleep 1
done
curl -sf "$GRAFANA_URL/api/health" >/dev/null || { echo "Grafana at $GRAFANA_URL is not answering" >&2; exit 1; }

panel_count='[.panels[]? | if .type == "row" then (.panels[]? // empty) else . end] | length'
imported=0
uids=""
for f in "$OUT"/*.json "$OUT"/external/*.json; do
  [ -e "$f" ] || continue
  uid=$(jq -r .uid "$f")
  resp=$(jq -c '{dashboard: ., overwrite: true}' "$f" |
    curl -sS -u "$GRAFANA_AUTH" -H 'Content-Type: application/json' -X POST --data-binary @- "$GRAFANA_URL/api/dashboards/db")
  if [ "$(jq -r '.status // empty' <<<"$resp")" != "success" ]; then
    echo "✗ $(basename "$f"): import refused: $resp" >&2
    exit 1
  fi
  curl -sSf -u "$GRAFANA_AUTH" "$GRAFANA_URL/api/dashboards/uid/$uid" > "$WORK/back.json"
  want_title=$(jq -r .title "$f")
  got_title=$(jq -r .dashboard.title "$WORK/back.json")
  want_panels=$(jq "$panel_count" "$f")
  got_panels=$(jq ".dashboard | $panel_count" "$WORK/back.json")
  if [ "$want_title" != "$got_title" ] || [ "$want_panels" != "$got_panels" ]; then
    echo "✗ $(basename "$f"): read back \"$got_title\" with $got_panels panels, want \"$want_title\" with $want_panels" >&2
    exit 1
  fi
  echo "✓ $(basename "$f"): uid $uid, \"$got_title\", $got_panels panels"
  imported=$((imported + 1))
  uids="$uids$uid"$'\n'
done
[ "$imported" -gt 0 ] || { echo "no dashboard was generated" >&2; exit 1; }
# Grafana keeps one dashboard per uid, so two files with one uid import as one dashboard.
distinct=$(printf '%s' "$uids" | sort -u | grep -c .)
if [ "$distinct" -ne "$imported" ]; then
  echo "✗ $imported files imported but only $distinct distinct uids: $(printf '%s' "$uids" | sort | uniq -d | tr '\n' ' ')" >&2
  exit 1
fi

step "lint and unit-test alert rules"
make -s -C "$ROOT" lint-alerts test-alerts

step "metric names are listed in ALLOWED"
check_names "$OUT"

printf '\nacceptance: PASS (%s dashboards imported)\n' "$imported"
