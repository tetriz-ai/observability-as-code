#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for the Makefile's lint-alerts and test-alerts gates: each runs against fixture rule
# files in a scratch tree and must fail on the input it exists to catch.
# Usage: scripts/test-lint-alerts.sh   (needs make, promtool, yq)

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
check() { local name=$1; shift; if "$@"; then pass=$((pass + 1)); echo "ok   $name"; else fail=$((fail + 1)); echo "FAIL $name"; fi; }

# rules writes alerts/rules-x.yaml in a fresh scratch tree from stdin.
rules() {
  rm -rf "$WORK/t"
  mkdir -p "$WORK/t/alerts/tests"
  cp "$ROOT/Makefile" "$WORK/t/"
  cat > "$WORK/t/alerts/rules-x.yaml"
}
lint() { local c=0; make -s -C "$WORK/t" lint-alerts "$@" >"$WORK/out" 2>&1 || c=$?; echo "$c"; }

rule() { # rule <labels-yaml-flow> [expr]
  printf 'spec:\n  groups:\n    - name: g\n      rules:\n        - alert: A\n          expr: %s\n          labels: %s\n' "${2:-up == 0}" "$1"
}

rule '{severity: p1, team: platform}' | rules
check "good rule: passes" test "$(lint)" = 0

rule '{severity: p1}' | rules
check "no routing label: fails" test "$(lint)" != 0
check "no routing label: names the alert" grep -q 'alerts missing a non-empty team label .*: A' "$WORK/out"

rule '{severity: p1, team: ""}' | rules
check "empty routing label: fails" test "$(lint)" != 0

rule '{severity: critical, team: platform}' | rules
check "severity outside the set: fails" test "$(lint)" != 0
check "severity outside the set: names the set" grep -q 'severity must be one of: p0 p1 p2: A' "$WORK/out"

rule '{team: platform}' | rules
check "no severity: fails" test "$(lint)" != 0

rule '{severity: critical, owner: db}' | rules
check "knobs: ALERT_ROUTING_LABEL and ALERT_SEVERITIES override the defaults" \
  test "$(lint ALERT_ROUTING_LABEL=owner 'ALERT_SEVERITIES=critical warning')" = 0

rule '{severity: p1, team: platform}' 'sum(rate(up[5m]) by (job)' | rules
check "unparseable PromQL: fails" test "$(lint)" != 0

printf 'spec:\n  groups: []\n' | rules
check "no rules reachable: fails" test "$(lint)" != 0
check "no rules reachable: says why" grep -q 'no rules reachable at .spec.groups\[\].rules\[\]' "$WORK/out"

printf 'groups:\n  - name: g\n    rules:\n      - alert: A\n        expr: up == 0\n        labels: {severity: p1, team: platform}\n' | rules
check "rules outside .spec (not a PrometheusRule): fails" test "$(lint)" != 0

rule '{severity: p1, team: platform}' | rules
rule '{severity: p1, team: platform}' 'sum(rate(up[5m]) by (job)' > "$WORK/t/alerts/rules-new.yml"
check ".yml rule file: linted too, fails on its unparseable PromQL" test "$(lint)" != 0
check ".yml rule file: named in the output" grep -q 'alerts/rules-new.yml' "$WORK/out"

{ rule '{severity: p1, team: platform}'; echo '---'; rule '{severity: p1, team: platform}'; } | rules
check "two YAML documents in one file: fails" test "$(lint)" != 0
check "two YAML documents in one file: says why" grep -q 'holds 2 YAML documents; promtool reads only the first' "$WORK/out"

{ rule '{severity: p1, team: platform}'; echo '---'; } | rules
check "a file ending in --- is one document: passes" test "$(lint)" = 0

{ echo '---'; rule '{severity: p1, team: platform}'; } | rules
check "a file starting with --- is one document: passes" test "$(lint)" = 0

{ rule '{severity: p1, team: platform}'; echo '---'; echo '# a document holding only a comment'; } | rules
check "a comment-only document does not count: passes" test "$(lint)" = 0

# test-alerts must fail when an expectation does not hold, and leave no extract behind.
rule '{severity: p1, team: platform}' | rules
cat > "$WORK/t/alerts/tests/x_test.yaml" <<'EOF'
rule_files: [../.extracted/rules-x.yaml]
evaluation_interval: 1m
tests:
  - interval: 1m
    input_series:
      - series: 'up{job="api"}'
        values: '1x10'
    alert_rule_test:
      - eval_time: 5m
        alertname: A
        exp_alerts:
          - exp_labels: {severity: p1, team: platform, job: api}
EOF
c=0; make -s -C "$WORK/t" test-alerts >"$WORK/out" 2>&1 || c=$?
check "test-alerts: a wrong expectation fails" test "$c" != 0
check "test-alerts: extract removed" test ! -e "$WORK/t/alerts/.extracted"
mv "$WORK/t/alerts/tests/x_test.yaml" "$WORK/t/alerts/tests/x_test.yml"
# A passing .yaml test beside it, so only the .yml file's failure can fail the target.
cat > "$WORK/t/alerts/tests/ok_test.yaml" <<'EOF'
rule_files: [../.extracted/rules-x.yaml]
evaluation_interval: 1m
tests:
  - interval: 1m
    input_series:
      - series: 'up{job="api"}'
        values: '1x10'
    alert_rule_test:
      - eval_time: 5m
        alertname: A
        exp_alerts: []
EOF
c=0; make -s -C "$WORK/t" test-alerts >"$WORK/out" 2>&1 || c=$?
check "test-alerts: a .yml test file runs too, and its wrong expectation fails" test "$c" != 0

# A .yml rule file is extracted for its tests like a .yaml one.
rm -rf "$WORK/t"
mkdir -p "$WORK/t/alerts/tests"
cp "$ROOT/Makefile" "$WORK/t/"
rule '{severity: p1, team: platform}' > "$WORK/t/alerts/rules-y.yml"
cat > "$WORK/t/alerts/tests/y_test.yaml" <<'EOF'
rule_files: [../.extracted/rules-y.yml]
evaluation_interval: 1m
tests:
  - interval: 1m
    input_series:
      - series: 'up{job="api"}'
        values: '0x10'
    alert_rule_test:
      - eval_time: 5m
        alertname: A
        exp_alerts:
          - exp_labels: {severity: p1, team: platform, job: api}
EOF
c=0; make -s -C "$WORK/t" test-alerts >"$WORK/out" 2>&1 || c=$?
check "test-alerts: a .yml rule file is extracted, and its passing test passes" test "$c" = 0

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
