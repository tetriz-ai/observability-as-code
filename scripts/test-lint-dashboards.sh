#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for the Makefile's lint-dashboards gate: a dashboard with one defect is added to a scratch
# copy of the repository, and the gate must fail on it. A clean dashboard must pass first, so a
# failure below is the defect's and not the scratch setup's.
# Usage: scripts/test-lint-dashboards.sh   (needs go, make, dashboard-linter)

set -euo pipefail

for bin in go make dashboard-linter; do
  command -v "$bin" >/dev/null 2>&1 || { echo "scripts/test-lint-dashboards.sh needs $bin on PATH" >&2; exit 2; }
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Resolved with pwd -P: the generator refuses an output path reached through a symbolic link, and
# the temporary directory on macOS is under one, /var.
WORK=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
check() { local name=$1; shift; if "$@"; then pass=$((pass + 1)); echo "ok   $name"; else fail=$((fail + 1)); echo "FAIL $name"; fi; }

# lint <description> <expr>: exit status of make lint-dashboards with one extra dashboard.
lint() {
  rm -rf "$WORK/t"
  mkdir -p "$WORK/t"
  cp -R "$ROOT"/{Makefile,go.mod,go.sum,.dashboard-linter.yaml,dashboards} "$WORK/t"/
  rm -rf "$WORK/t/dashboards/output"
  cat > "$WORK/t/dashboards/zz_test_dashboard.go" <<EOF
package main

import "github.com/grafana/grafana-foundation-sdk/go/dashboard"

func lintTestDashboard() *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder("Lint Test").
		Uid("lint-test").
		Readonly().
		WithVariable(datasourceVariable()).
		WithVariable(jobVariable("process_cpu_seconds_total")).
		WithPanel(defaultTimeseries().Title("CPU").Description("$1").Unit("short").
			WithTarget(promQuery(\`$2\`, "{{instance}}")))
}
EOF
  awk '{ print } /builder: redisCommandDashboard\(\)\},/ { print "\t\t{name: \"lint-test\", builder: lintTestDashboard()}," }' \
    "$ROOT/dashboards/main.go" > "$WORK/t/dashboards/main.go"
  local c=0
  make -s -C "$WORK/t" lint-dashboards DASHBOARDS_OUT="$WORK/t/dashboards/output" >"$WORK/out" 2>&1 || c=$?
  echo "$c"
}

good="sum by (instance) (rate(process_cpu_seconds_total{job=~\"\$job\"}[\$__rate_interval]))"
check "clean dashboard: passes" test "$(lint "CPU seconds per second." "$good")" = 0
check "clean dashboard: was linted" grep -q 'linting .*/lint-test.json' "$WORK/out"
check "panel without a description: fails" test "$(lint "" "$good")" != 0
check "query without the \$job filter: fails" \
  test "$(lint "CPU seconds per second." "sum by (instance) (rate(process_cpu_seconds_total[\$__rate_interval]))")" != 0
check "fixed [5m] rate window: fails" \
  test "$(lint "CPU seconds per second." "sum by (instance) (rate(process_cpu_seconds_total{job=~\"\$job\"}[5m]))")" != 0

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
