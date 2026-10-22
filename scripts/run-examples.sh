#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Runs every example under examples/ and compares its standard output with the last ```text
# block of its README; make examples runs it. The command run is the README's ```sh block (make
# examples EXAMPLE=<name>), from the repository root,
# so a stale path in the copyable instruction fails here. Spaces at the end of a line and
# trailing blank lines are ignored; nothing else is normalised. Fails when the index and the
# folders disagree, or when a block the top-level README quotes from an example differs from it.
# Needs: go, make, jq, yq, promtool, dashboard-linter, shellcheck, patch.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
fail=0
SCRATCH=$(mktemp -d)
# dashboards/output/ is the user's to keep: removed afterwards only if this run created it.
had_output=0
[ -d dashboards/output ] && had_output=1
trap 'rm -rf "$SCRATCH"; if [ "$had_output" = 0 ]; then rm -rf "$ROOT/dashboards/output"; fi' EXIT

listed=$(grep -oE '\]\([a-z0-9-]+/\)' examples/README.md | sed -E 's/^\]\(//; s/\/\)$//' | sort)
present=$(find examples -mindepth 1 -maxdepth 1 -type d | sed 's#^examples/##' | sort)
if [ "$listed" != "$present" ]; then
  echo "✗ examples/README.md lists [$(tr '\n' ' ' <<<"$listed")] but the folders are [$(tr '\n' ' ' <<<"$present")]" >&2
  fail=1
fi

go mod download
go vet ./...
shellcheck -x scripts/*.sh dashboards/external/scripts/sync.sh examples/*/run.sh

# block <file> <lang> <first|last>: the body of the first or last fenced block in that language.
block() {
  awk -v lang="$2" -v which="$3" '
    $0 == "```" lang { inb = 1; cur = ""; next }
    inb && $0 == "```" { inb = 0; n++; if (which == "first" && n == 1) { printf "%s", cur; done = 1; exit } last = cur; next }
    inb { cur = cur $0 "\n" }
    END { if (which == "last" && !done) printf "%s", last }' "$1"
}
trim() { sed -e 's/[[:space:]]*$//' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'; }

# nth <file> <lang> <n>: the body of the n-th fenced block in that language.
nth() {
  awk -v lang="$2" -v want="$3" '
    $0 == "```" lang { inb = 1; n++; next }
    inb && $0 == "```" { inb = 0; next }
    inb && n == want { print }' "$1"
}
same() { # same <description> <file a> <file b>
  if diff -u "$2" "$3" > "$SCRATCH/diff"; then echo "✓ README quotes $1"; else
    echo "✗ README's copy of $1 differs:" >&2; cat "$SCRATCH/diff" >&2; fail=1; fi
}

# The top-level README's quick start is examples/generate-dashboards: the same command and the
# same output. Its first two lines need the published remote, so they are skipped by name.
nth README.md sh 1 > "$SCRATCH/qs"
for skip in "git clone https://github.com/tetriz-ai/observability-as-code" "cd observability-as-code"; do
  grep -qxF "$skip" "$SCRATCH/qs" && echo "  skip \"$skip\": needs the published repository"
done
grep -vxF -e "git clone https://github.com/tetriz-ai/observability-as-code" -e "cd observability-as-code" "$SCRATCH/qs" > "$SCRATCH/qs.cmd"
grep -v -e '^#!' -e '^source ' -e '^# Copyright 2026 Workloom Solutions Private Limited$' -e '^# SPDX-License-Identifier: MIT$' -e '^$' examples/generate-dashboards/run.sh > "$SCRATCH/ex.cmd"
same "the quick-start command" "$SCRATCH/ex.cmd" "$SCRATCH/qs.cmd"
nth README.md text 1 > "$SCRATCH/qs.out"
block examples/generate-dashboards/README.md text last > "$SCRATCH/ex.out"
same "the quick-start output" "$SCRATCH/ex.out" "$SCRATCH/qs.out"
nth README.md yaml 1 > "$SCRATCH/rule"
same "examples/add-an-alert-rule/rules-checkout.yaml" examples/add-an-alert-rule/rules-checkout.yaml "$SCRATCH/rule"
nth README.md yaml 2 > "$SCRATCH/test"
head -n "$(wc -l < "$SCRATCH/test")" examples/add-an-alert-rule/checkout_alerts_test.yaml > "$SCRATCH/test.ex"
same "the first case of examples/add-an-alert-rule/checkout_alerts_test.yaml" "$SCRATCH/test.ex" "$SCRATCH/test"
nth README.md go 1 > "$SCRATCH/go"
sed -n '/^package main$/,$p' examples/add-a-dashboard/go_runtime.go > "$SCRATCH/go.ex"
same "examples/add-a-dashboard/go_runtime.go" "$SCRATCH/go.ex" "$SCRATCH/go"
nth README.md go 2 > "$SCRATCH/reg"
sed -n 's/^+//p' examples/add-a-dashboard/register.patch | grep -v '^++' > "$SCRATCH/reg.ex"
same "the line examples/add-a-dashboard/register.patch adds" "$SCRATCH/reg.ex" "$SCRATCH/reg"

for name in $present; do
  readme="examples/$name/README.md"
  if [ ! -f "$readme" ]; then echo "✗ $name: no $readme" >&2; fail=1; continue; fi
  cmd=$(block "$readme" sh first)
  if [ -z "$cmd" ]; then echo "✗ $name: no sh block in $readme" >&2; fail=1; continue; fi
  want=$(block "$readme" text last | trim)
  got=$(bash -c "$cmd" 2>"$SCRATCH/stderr" | trim) || { echo "✗ $name: '$cmd' exited non-zero" >&2; cat "$SCRATCH/stderr" >&2; fail=1; continue; }
  if [ "$got" = "$want" ]; then
    echo "✓ $name"
  else
    echo "✗ $name: output differs from $readme" >&2
    diff <(echo "$want") <(echo "$got") >&2 || true
    fail=1
  fi
done

[ "$fail" -eq 0 ] && echo "examples: all match"
exit "$fail"
