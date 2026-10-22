#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for scripts/metric-names.sh, the metric-name extraction scripts/acceptance.sh checks
# against ALLOWED, and for that check over every generated dashboard, community ones included.
# Usage: scripts/test-metric-names.sh   (needs bash, sed, grep, go, jq, yq)

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Resolved with pwd -P: the generator refuses an output path reached through a symbolic link, and
# the temporary directory on macOS is under one, /var.
WORK=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; }

# run <expressions>: runs the extraction on them, one per line; leaves the names (one line,
# space-separated) in $WORK/names, standard error in $WORK/err and the exit status in $WORK/code.
run() {
  local c=0
  printf '%s\n' "$1" | bash "$ROOT/scripts/metric-names.sh" >"$WORK/out" 2>"$WORK/err" || c=$?
  echo "$c" >"$WORK/code"
  paste -sd' ' "$WORK/out" >"$WORK/names"
}
yields() { # yields <expression> <the names it reads, sorted, space-separated>
  run "$1"
  if [ "$(cat "$WORK/code")" = 0 ] && [ "$(cat "$WORK/names")" = "$2" ]; then ok "$1 -> $2"; else
    bad "$1 -> want '$2', got '$(cat "$WORK/names")' (exit $(cat "$WORK/code"))"; fi
}
refused() { # refused <expression>
  run "$1"
  if [ "$(cat "$WORK/code")" = 1 ] && grep -qxF "name the metric outside the matcher: $1" "$WORK/err" &&
     [ ! -s "$WORK/out" ]; then ok "refused: $1"; else
    bad "refused: $1 (exit $(cat "$WORK/code"), names '$(cat "$WORK/names")', stderr '$(cat "$WORK/err")')"; fi
}

echo "# a metric named inside a matcher is refused"
refused 'rate({__name__="unlisted_metric_total"}[5m]) > 0'
refused '{"unlisted_metric_total"}'
refused 'sum by (job) ({ "unlisted_metric_total", job="api"})'
refused 'count({__name__=~"unlisted_.+"})'
run $'up\nrate({__name__="unlisted_metric_total"}[5m])'
check_one() { [ "$(cat "$WORK/code")" = 1 ] && [ ! -s "$WORK/out" ] && [ "$(grep -c . "$WORK/err")" = 1 ]; }
if check_one; then ok "one refused expression among others: exit 1, no names, only it named"; else
  bad "one refused expression among others (exit $(cat "$WORK/code"), names '$(cat "$WORK/names")')"; fi

echo "# names"
yields 'rate(unlisted_metric_total[5m]) > 0' 'unlisted_metric_total'
yields "sum by (job) (rate(x_total{job=~\"\$job\", le=\"2.5\"}[\$__rate_interval] offset 1h))" 'x_total'
yields "label_replace(up, \"dst\", \"\$1\", \"src\", \"(.*)\")" 'up'
yields 'a_total / on (job) group_left (team) b_info' 'a_total b_info'
yields 'up > bool 0 and on () vector(1) or absent(down) unless up' 'down up'
yields 'up atan2 up' 'up'
run $'b_total\na_total\nb_total'
if [ "$(cat "$WORK/names")" = "a_total b_total" ]; then ok "names are sorted and listed once"; else
  bad "names are sorted and listed once: got '$(cat "$WORK/names")'"; fi

echo "# numbers and keywords are not names"
yields 'up > 1e3' 'up'
yields 'up > 1e-9' 'up'
yields 'up > 2.5E+3' 'up'
yields 'up > .5e3' 'up'
yields 'up > 0x10' 'up'
yields 'up * 1.5 + 0.25' 'up'
yields 'topk(5, up)' 'up'
yields 'log10(up)' 'up'
yields 'up > Inf' 'up'
yields 'up > -inf' 'up'
yields 'up != NaN' 'up'
yields 'up AND up' 'up'
yields 'unlisted2 > 0' 'unlisted2'
yields 'unlisted_v1e3_total > 1e3' 'unlisted_v1e3_total'
yields 'x_total unless on (job, queue) ALERTS{alertname="A", alertstate="firing"}' 'ALERTS x_total'

echo "# acceptance checks a community dashboard's metric names too"
OUT="$WORK/generated"
(cd "$ROOT" && go run ./dashboards -output "$OUT") >/dev/null
gate() { # gate <case> <want exit> [<the name the failure must list>]
  local c=0
  bash "$ROOT/scripts/acceptance.sh" --check-names "$OUT" >"$WORK/gate.out" 2>"$WORK/gate.err" || c=$?
  if [ "$c" = "$2" ] && { [ -z "${3:-}" ] || grep -qx "$3" "$WORK/gate.err"; }; then ok "$1"; else
    bad "$1 (exit $c, stderr '$(paste -sd' ' "$WORK/gate.err")')"; fi
}
gate "the Go-built dashboards alone: exit 0" 0
mkdir -p "$OUT/external"
printf '{"uid": "c", "title": "C", "panels": [{"targets": [{"expr": "rate(unknown_community_metric_total[5m])"}]}]}\n' > "$OUT/external/c.json"
gate "a community dashboard reading an unlisted metric: exit 1, naming it" 1 unknown_community_metric_total
printf '{"uid": "c", "title": "C", "templating": {"list": [{"type": "query", "query": "label_values(unknown_variable_metric, job)"}]}}\n' > "$OUT/external/c.json"
gate "a community dashboard variable on an unlisted metric: exit 1, naming it" 1 unknown_variable_metric
printf '{"uid": "c", "title": "C", "panels": [{"targets": [{"expr": "sum(asynq_queue_size)"}]}]}\n' > "$OUT/external/c.json"
gate "a community dashboard reading listed metrics: exit 0" 0

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
