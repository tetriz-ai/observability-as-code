#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for dashboards/external/scripts/sync.sh and for `make dashboards` clearing stale
# output. A fake curl on PATH serves fixture dashboards, so nothing touches the network.
# Usage: scripts/test-sync.sh   (needs bash, jq, yq, go, make)

set -euo pipefail

for bin in jq yq go make; do
  command -v "$bin" >/dev/null 2>&1 || { echo "scripts/test-sync.sh needs $bin on PATH" >&2; exit 2; }
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Resolved with pwd -P: the generator refuses an output path reached through a symbolic link, and
# the temporary directory on macOS is under one, /var.
WORK=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; }
check() { local name=$1; shift; if "$@"; then ok "$name"; else bad "$name"; fi; }

# Fake curl: serves $WORK/served/<id>.json when present, a minimal dashboard otherwise; logs
# each id to $WORK/curl.log; sleeps FAKE_CURL_SLEEP seconds first. For the id in FAKE_CURL_FAIL
# it answers an HTTP error the way curl does: exit 22 under -f, otherwise the server's JSON
# error body and exit 0.
mkdir -p "$WORK/bin" "$WORK/tmp"
cat > "$WORK/bin/curl" <<'EOF'
#!/usr/bin/env bash
url="${*: -1}"
id=$(printf '%s' "$url" | sed -E 's#.*/dashboards/([0-9]+)/.*#\1#')
echo "$id" >> "$WORK/curl.log"
echo "$url" >> "$WORK/urls.log"
[ -n "${FAKE_CURL_SLEEP:-}" ] && sleep "$FAKE_CURL_SLEEP"
if [ "${FAKE_CURL_FAIL:-}" = "$id" ]; then
  for a in "$@"; do
    case "$a" in --fail|-[!-]*f*) echo "curl: (22) The requested URL returned error: 404" >&2; exit 22 ;; esac
  done
  echo '{"message": "Dashboard not found"}'
  exit 0
fi
if [ -f "$WORK/served/$id.json" ]; then cat "$WORK/served/$id.json"; else
  printf '{"id": 7, "uid": "upstream-%s", "title": "Dashboard %s", "panels": []}\n' "$id" "$id"; fi
EOF
# Fake mktemp: the real one, but always under $WORK/tmp, so a leftover temp file is visible.
REAL_MKTEMP=$(command -v mktemp)
cat > "$WORK/bin/mktemp" <<EOF
#!/usr/bin/env bash
exec "$REAL_MKTEMP" "\$WORK/tmp/tmp.XXXXXXXX"
EOF
chmod +x "$WORK/bin/curl" "$WORK/bin/mktemp"
export WORK

# setup copies sync.sh (and normalize.jq when present) into a fresh tree and writes the
# manifest given on stdin.
setup() {
  rm -rf "$WORK/t" "$WORK/served" "$WORK/curl.log" "$WORK/urls.log" "$WORK/tmp"
  mkdir -p "$WORK/t/external/scripts" "$WORK/served" "$WORK/tmp"
  touch "$WORK/curl.log"
  cp "$ROOT/dashboards/external/scripts/"* "$WORK/t/external/scripts/"
  cat > "$WORK/t/external/manifest.yaml"
}
X="$WORK/t/external"
sync() { PATH="$WORK/bin:$PATH" bash "$X/scripts/sync.sh" "$@" >"$WORK/out" 2>"$WORK/err"; }
code() { local c=0; sync "$@" || c=$?; echo "$c"; }
json_count() { find "$X" -maxdepth 1 -name '*.json' | wc -l | tr -d ' '; }

TWO=$'dashboards:\n  - name: pods\n    grafana_com_id: 101\n    uid: k8s-pods\n  - name: pg\n    grafana_com_id: 202\n    uid: postgres\n'

echo "# fetch, skip, refresh, only"
printf '%s' "$TWO" | setup
check "add: exit 0" test "$(code)" = 0
check "add: both written" test "$(json_count)" = 2
check "add: uid pinned" test "$(jq -r .uid "$X/pods.json")" = k8s-pods
check "add: id removed" test "$(jq 'has("id")' "$X/pods.json")" = false
check "add: no temp file left" test -z "$(ls -A "$WORK/tmp")"
: > "$WORK/curl.log"
check "rerun: exit 0" test "$(code)" = 0
check "rerun: existing files skipped" grep -q 'pods: exists, skipping' "$WORK/out"
check "rerun: nothing fetched" test ! -s "$WORK/curl.log"
check "--refresh: exit 0" test "$(code --refresh)" = 0
check "--refresh: both fetched" test "$(wc -l < "$WORK/curl.log" | tr -d ' ')" = 2
: > "$WORK/curl.log"
check "--only: exit 0" test "$(code --refresh --only pg)" = 0
check "--only: one fetched" test "$(cat "$WORK/curl.log")" = 202
check "--only unknown: exit 1" test "$(code --only nope)" = 1
check "--only unknown: says so" grep -q "no manifest entry matched --only 'nope'" "$WORK/err"
check "bad flag: exit 2" test "$(code --nope)" = 2
for h in -h --help; do
  check "$h: exit 0" test "$(code "$h")" = 0
  check "$h: prints the usage" grep -q '^Usage: ' "$WORK/out"
done

echo "# orphans, failures, missing tools"
printf '%s' "$TWO" | setup
echo '{}' > "$X/extra.json"
code >/dev/null
check "orphan: warned" grep -q 'orphan: extra.json exists but is not in manifest.yaml' "$WORK/err"
printf '%s' "$TWO" | setup
check "fetch failure: exit 1" test "$(FAKE_CURL_FAIL=101 code)" = 1
check "fetch failure: failed entry not written" test ! -e "$X/pods.json"
check "fetch failure: other entry written" test -e "$X/pg.json"
check "fetch failure: no temp file left" test -z "$(ls -A "$WORK/tmp")"
for body in empty null array two-values empty-object numeric-title; do
  printf 'dashboards:\n  - name: odd\n    grafana_com_id: 301\n    uid: odd\n  - name: pg\n    grafana_com_id: 202\n    uid: postgres\n' | setup
  case "$body" in
    empty) : > "$WORK/served/301.json" ;;
    null) echo null > "$WORK/served/301.json" ;;
    array) echo '[1, 2]' > "$WORK/served/301.json" ;;
    two-values) echo '{"title":"A"}{"title":"B"}' > "$WORK/served/301.json" ;;
    empty-object) echo '{}' > "$WORK/served/301.json" ;;
    numeric-title) echo '{"title":5}' > "$WORK/served/301.json" ;;
  esac
  check "$body body: exit 1" test "$(code)" = 1
  check "$body body: not written" test ! -e "$X/odd.json"
  check "$body body: other entry written" test -e "$X/pg.json"
done
printf 'dashboards:\n  - name: bom\n    grafana_com_id: 302\n    uid: bom\n' | setup
printf '\357\273\277{"title": "With a BOM"}\n' > "$WORK/served/302.json"
check "body with a UTF-8 BOM: exit 0" test "$(code)" = 0
check "body with a UTF-8 BOM: written without the BOM" test "$(head -c 1 "$X/bom.json" 2>/dev/null)" = "{"
check "body with a UTF-8 BOM: title kept" test "$(jq -r .title "$X/bom.json" 2>/dev/null)" = "With a BOM"
mkdir -p "$WORK/nojq"
for t in bash sed dirname sort uniq grep mv rm head tr cat yq; do ln -sf "$(command -v "$t")" "$WORK/nojq/$t"; done
ln -sf "$WORK/bin/curl" "$WORK/nojq/curl"
c=0; PATH="$WORK/nojq" "$WORK/nojq/bash" "$X/scripts/sync.sh" >/dev/null 2>"$WORK/err" || c=$?
check "missing jq: exit 2" test "$c" = 2
check "missing jq: says so" grep -q 'missing required binary: jq' "$WORK/err"

echo "# manifest validation"
printf 'dashboards:\n  - name: ok\n    grafana_com_id: 1\n    uid: ok\n  - name: no-uid\n    grafana_com_id: 111\n' | setup
check "missing uid: exit 1" test "$(code)" = 1
check "missing uid: names the entry" grep -q 'manifest entry 2 (name: "no-uid", uid: "") needs both a name and a uid' "$WORK/err"
check "missing uid: nothing written" test "$(json_count)" = 0
check "missing uid: nothing fetched" test ! -s "$WORK/curl.log"
printf 'dashboards:\n  - grafana_com_id: 222\n    uid: no-name\n' | setup
check "missing name: exit 1" test "$(code)" = 1
check "missing name: names the entry" grep -q 'manifest entry 1 (name: "", uid: "no-name") needs both a name and a uid' "$WORK/err"
check "missing name: nothing written" test "$(json_count)" = 0
printf 'dashboards:\n  - name: a\n    grafana_com_id: 1\n    uid: same\n  - name: b\n    grafana_com_id: 2\n    uid: same\n' | setup
check "duplicate uid: exit 1" test "$(code)" = 1
check "duplicate uid: names both entries" grep -q 'uid "same" is used by more than one manifest entry: a (grafana.com/1), b (grafana.com/2)' "$WORK/err"
check "duplicate uid: nothing written" test "$(json_count)" = 0
check "duplicate uid: nothing fetched" test ! -s "$WORK/curl.log"

printf 'dashboards:\n  - name: a\n    grafana_com_id: 1\n    uid: one\n  - name: a\n    grafana_com_id: 2\n    uid: two\n' | setup
check "duplicate name: exit 1" test "$(code)" = 1
check "duplicate name: names both entries" grep -q 'name "a" is used by more than one manifest entry: a (grafana.com/1), a (grafana.com/2)' "$WORK/err"
check "duplicate name: nothing fetched" test ! -s "$WORK/curl.log"
reject() { # reject <case> <manifest entry yaml> <expected message>
  printf 'dashboards:\n  - %s\n' "$2" | setup
  check "$1: exit 1" test "$(code)" = 1
  check "$1: says why" grep -qF "$3" "$WORK/err"
  check "$1: nothing fetched or written" test ! -s "$WORK/curl.log" -a "$(json_count)" = 0 -a ! -e "$WORK/t/escaped.json"
}
for id in '"12/../../other"' '"123"' '0' '-1' '1.5' 'true' '[]' '{}'; do
  reject "invalid grafana_com_id $id" "{name: a, grafana_com_id: $id, uid: a}" 'needs a positive integer grafana_com_id'
done
for revision in '"../latest"' '"4"' '0' '-1' '1.5' 'true' '[]' '{}'; do
  reject "invalid revision $revision" "{name: a, grafana_com_id: 1, uid: a, revision: $revision}" 'needs a positive integer revision when set'
done
printf 'dashboards:\n  - {name: pinned, grafana_com_id: 101, uid: pinned, revision: 7}\n' | setup
check "pinned revision: exit 0" test "$(code)" = 0
check "pinned revision: exact URL" grep -qF '/dashboards/101/revisions/7/download' "$WORK/urls.log"
printf '%s' "$TWO" | setup
check "default revision: exit 0" test "$(code)" = 0
check "default revision: latest URL" grep -qF '/dashboards/101/revisions/latest/download' "$WORK/urls.log"
reject "uid with a space and a slash" '{name: a, grafana_com_id: 1, uid: "k8s pods/x"}' 'has a uid Grafana rejects: use 1 to 40 letters'
reject "uid of 41 characters" '{name: a, grafana_com_id: 1, uid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}' 'has a uid Grafana rejects'
reject "name leaving the folder" '{name: ../escaped, grafana_com_id: 1, uid: esc}' 'manifest entry 1 (name: "../escaped", uid: "esc") has a name other than letters'
reject "name .." '{name: "..", grafana_com_id: 1, uid: dots}' 'has a name other than letters'
reject "missing grafana_com_id" '{name: a, uid: a}' 'manifest entry 1 (name: "a", uid: "a") needs a positive integer grafana_com_id'
reject "name starting with _" '{name: _team, grafana_com_id: 1, uid: team}' "has a name starting with '.', '_' or '-'"
reject "name starting with ." '{name: .hidden, grafana_com_id: 1, uid: hidden}' "has a name starting with '.', '_' or '-'"
reject "name starting with -" '{name: "-x", grafana_com_id: 1, uid: dash}' "has a name starting with '.', '_' or '-'"

echo "# names with '.', '_' and '-' inside"
printf 'dashboards:\n  - name: x_\n    grafana_com_id: 401\n    uid: x-under\n  - name: x.y\n    grafana_com_id: 402\n    uid: x-dot\n  - name: x-y\n    grafana_com_id: 403\n    uid: x-dash\n' | setup
check "x_, x.y and x-y: exit 0" test "$(code)" = 0
check "x_ fetched" test -e "$X/x_.json"
check "x.y fetched" test -e "$X/x.y.json"
check "x-y fetched" test -e "$X/x-y.json"
echo '{}' > "$X/x..json"
echo '{}' > "$X/-x.json"
code >/dev/null
check "orphan x..json beside the name x_: warned ('.' is not a wildcard)" grep -q 'orphan: x..json exists' "$WORK/err"
check "orphan -x.json: warned" grep -q 'orphan: -x.json exists' "$WORK/err"
check "orphan -x.json: no grep usage error" test -z "$(grep -i 'usage' "$WORK/err")"

echo "# datasource inputs"
printf 'dashboards:\n  - name: pv\n    grafana_com_id: 13646\n    uid: k8s-pvc\n' | setup
cat > "$WORK/served/13646.json" <<'EOF'
{"id": 3, "uid": "x", "title": "PV",
 "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginId": "prometheus"},
              {"name": "DS_LOKI", "label": "Loki", "type": "datasource", "pluginId": "loki"},
              {"name": "VAR_X", "type": "constant", "value": "1"}],
 "panels": [{"title": "used", "datasource": "${DS_PROMETHEUS}"}],
 "templating": {"list": [{"name": "DS_LOKI", "type": "datasource", "query": "loki"}]}}
EOF
code >/dev/null
check "inputs: undefined datasource input becomes a variable" \
  test "$(jq -c '[.templating.list[] | select(.name=="DS_PROMETHEUS") | {type, query, "label": .label}]' "$X/pv.json")" = '[{"type":"datasource","query":"prometheus","label":"Prometheus"}]'
check "inputs: an input the dashboard defines is not duplicated" \
  test "$(jq '[.templating.list[] | select(.name=="DS_LOKI")] | length' "$X/pv.json")" = 1
check "inputs: non-datasource inputs are left alone" \
  test "$(jq '[.templating.list[] | select(.name=="VAR_X")] | length' "$X/pv.json")" = 0
check "inputs: panels untouched" test "$(jq -r '.panels[0].datasource' "$X/pv.json")" = "\${DS_PROMETHEUS}"
# The k8s-views shape: DS_PROMETHEUS is declared in __inputs, but the panels use the dashboard's own
# datasource variable, so no second, unused datasource dropdown is added.
printf 'dashboards:\n  - name: kv\n    grafana_com_id: 15757\n    uid: k8s-views\n' | setup
cat > "$WORK/served/15757.json" <<'EOF'
{"id": 4, "uid": "x", "title": "K8s views",
 "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginId": "prometheus"}],
 "panels": [{"title": "pods", "datasource": {"type": "prometheus", "uid": "${datasource}"}}],
 "templating": {"list": [{"name": "datasource", "type": "datasource", "query": "prometheus"}]}}
EOF
code >/dev/null
check "inputs: an input nothing uses gets no variable" \
  test "$(jq -c '[.templating.list[].name]' "$X/kv.json")" = '["datasource"]'
# An input only a variable uses (a query variable's datasource) still becomes a variable.
printf 'dashboards:\n  - name: vq\n    grafana_com_id: 15758\n    uid: var-query\n' | setup
cat > "$WORK/served/15758.json" <<'EOF'
{"id": 5, "uid": "x", "title": "Variable query",
 "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginId": "prometheus"}],
 "panels": [],
 "templating": {"list": [{"name": "job", "type": "query", "datasource": {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}}]}}
EOF
code >/dev/null
check "inputs: an input a variable uses becomes a variable" \
  test "$(jq -c '[.templating.list[].name]' "$X/vq.json")" = '["job","DS_PROMETHEUS"]'
# input_ref <case> <panels JSON> [<annotations JSON>] <expected templating names>: one dashboard
# declaring DS_PROMETHEUS in __inputs, with the dashboard's own datasource variable and the given
# panels and annotations; checks the variable names normalize.jq leaves.
input_ref() {
  local name=$1 panels=$2 annotations=$3 want=$4
  printf 'dashboards:\n  - name: ref\n    grafana_com_id: 15759\n    uid: ref\n' | setup
  cat > "$WORK/served/15759.json" <<EOF
{"id": 6, "uid": "x", "title": "Reference forms",
 "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginId": "prometheus"}],
 "panels": $panels, "annotations": {"list": $annotations},
 "templating": {"list": [{"name": "datasource", "type": "datasource", "query": "prometheus"}]}}
EOF
  code >/dev/null
  check "inputs: $name" test "$(jq -c '[.templating.list[].name]' "$X/ref.json")" = "$want"
}
for form in "\$DS_PROMETHEUS" "[[DS_PROMETHEUS]]" "\${DS_PROMETHEUS:raw}"; do
  input_ref "a panel using $form adds the variable" "[{\"title\": \"p\", \"datasource\": \"$form\"}]" '[]' '["datasource","DS_PROMETHEUS"]'
done
input_ref "a reference only in an annotation adds the variable" '[]' \
  "[{\"name\": \"deploys\", \"datasource\": {\"type\": \"prometheus\", \"uid\": \"\${DS_PROMETHEUS}\"}}]" '["datasource","DS_PROMETHEUS"]'
input_ref "the name in prose, not a reference, adds none" \
  "[{\"title\": \"p\", \"description\": \"Imported from DS_PROMETHEUS\", \"datasource\": \"\${datasource}\"}]" '[]' '["datasource"]'

echo "# interrupted fetch"
printf '%s' "$TWO" | setup
FAKE_CURL_SLEEP=3 PATH="$WORK/bin:$PATH" bash "$X/scripts/sync.sh" --only pods >/dev/null 2>&1 &
pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do [ -n "$(ls -A "$WORK/tmp")" ] && break; sleep 0.2; done
check "TERM: temp file existed mid-fetch" test -n "$(ls -A "$WORK/tmp")"
kill -TERM "$pid"
c=0; wait "$pid" || c=$?
check "TERM: exit 143" test "$c" = 143
check "TERM: temp file removed" test -z "$(ls -A "$WORK/tmp")"
check "TERM: nothing written" test "$(json_count)" = 0

echo "# make dashboards removes only the files it wrote before"
OUT="$WORK/out-dir"
make -s -C "$ROOT" dashboards DASHBOARDS_OUT="$OUT" >/dev/null
check "first run: .generated lists what it wrote" \
  test "$(paste -sd' ' "$OUT/.generated")" = "asynq-queue-health.json redis-command-health.json"
# Files an earlier run wrote and lists, which this run no longer writes, and files it never wrote.
mkdir -p "$OUT/external" "$OUT/archive"
for f in removed-dashboard.json .removed-hidden.json external/removed-from-manifest.json external/.removed-hidden.json; do
  echo '{}' > "$OUT/$f"
  echo "$f" >> "$OUT/.generated"
done
echo 'mine' > "$OUT/notes.json"
echo 'mine' > "$OUT/external/notes.json"
echo '{}' > "$OUT/archive/keep.json"
make -s -C "$ROOT" dashboards DASHBOARDS_OUT="$OUT" >/dev/null
check "a stale dashboard it wrote removed" test ! -e "$OUT/removed-dashboard.json"
check "a stale community copy it wrote removed" test ! -e "$OUT/external/removed-from-manifest.json"
check "stale files it wrote whose names start with . removed" test ! -e "$OUT/.removed-hidden.json" -a ! -e "$OUT/external/.removed-hidden.json"
check "generated dashboards present" test -e "$OUT/asynq-queue-health.json"
check "a JSON file it never wrote kept" test "$(cat "$OUT/notes.json" 2>/dev/null)" = mine
check "a JSON file in external/ it never wrote kept" test "$(cat "$OUT/external/notes.json" 2>/dev/null)" = mine
check "JSON in another subdirectory of the output kept" test -e "$OUT/archive/keep.json"
check "the removed files are off .generated" test -z "$(grep removed "$OUT/.generated")"
# An output path with a space is one path.
echo 'keep' > "$WORK/victim"
c=0; make -s -C "$ROOT" dashboards DASHBOARDS_OUT="$WORK/victim dir/out" >/dev/null 2>&1 || c=$?
check "output path with a space: exit 0" test "$c" = 0
check "output path with a space: the file named like its first word kept" test -e "$WORK/victim"
check "output path with a space: dashboards written there" test -e "$WORK/victim dir/out/asynq-queue-health.json"

echo "# make dashboards refuses an output path with a symbolic link in it, before removing anything"
# outside holds JSON and a .generated that lists it, so a run that followed a link and cleaned
# there would delete it.
mkdir -p "$WORK/outside/external" "$WORK/outside/sub" "$WORK/o1"
echo keep > "$WORK/outside/keep.json"
echo keep > "$WORK/outside/external/keep2.json"
echo keep > "$WORK/outside/sub/keep3.json"
printf 'keep.json\nexternal/keep2.json\n' > "$WORK/outside/.generated"
printf 'keep3.json\n' > "$WORK/outside/sub/.generated"
ln -s "$WORK/outside" "$WORK/o1/external"
ln -s "$WORK/outside" "$WORK/o2"
symlinked() { # symlinked <case> <DASHBOARDS_OUT>
  local c=0
  make -s -C "$ROOT" dashboards DASHBOARDS_OUT="$2" >/dev/null 2>"$WORK/err" || c=$?
  check "$1: fails" test "$c" != 0
  check "$1: the generator names the link" grep -qF "is a symbolic link or is reached through one" "$WORK/err"
  check "$1: the generator exits 2" grep -qx 'exit status 2' "$WORK/err"
  check "$1: the files outside kept" test "$(cat "$WORK/outside/keep.json" "$WORK/outside/external/keep2.json" "$WORK/outside/sub/keep3.json" 2>/dev/null | paste -sd' ' -)" = "keep keep keep"
  check "$1: nothing generated" test -z "$(find "$WORK/o1" "$WORK/outside" -name 'asynq-queue-health.json')"
}
symlinked "output/external a symbolic link" "$WORK/o1"
symlinked "output a symbolic link" "$WORK/o2"
symlinked "output a symbolic link, with /." "$WORK/o2/."
symlinked "output a symbolic link, with a trailing slash" "$WORK/o2/"
symlinked "a symbolic link above the output" "$WORK/o2/sub"

echo "# make dashboards copies every community file"
rm -rf "$WORK/tree"
mkdir -p "$WORK/tree"
cp -R "$ROOT"/{Makefile,go.mod,go.sum,dashboards} "$WORK/tree"/
rm -rf "$WORK/tree/dashboards/output"
echo '{"uid": "team", "title": "Team"}' > "$WORK/tree/dashboards/external/_team.json"
printf '\0\5\26\7junk' > "$WORK/tree/dashboards/external/._team.json"
c=0; make -s -C "$WORK/tree" dashboards >/dev/null 2>&1 || c=$?
check "make dashboards beside a macOS ._team.json: exit 0" test "$c" = 0
check "a file named _team.json is copied" test -e "$WORK/tree/dashboards/output/external/_team.json"
check "the ._team.json metadata file is not copied" test ! -e "$WORK/tree/dashboards/output/external/._team.json"

echo "# make dashboards refuses an output that would clear the source"
echo '{"uid": "keep", "title": "Keep"}' > "$WORK/tree/dashboards/external/keep.json"
refuse_out() { # refuse_out <DASHBOARDS_OUT> <expected message> [<name in the case names>]
  local c=0 name="DASHBOARDS_OUT='${3:-$1}'"
  make -s -C "$WORK/tree" dashboards DASHBOARDS_OUT="$1" >/dev/null 2>"$WORK/err" || c=$?
  check "$name: exit 2" test "$c" = 2
  check "$name: says why" grep -qF -- "$2" "$WORK/err"
  check "$name: fetched JSON kept" test -e "$WORK/tree/dashboards/external/keep.json"
  check "$name: nothing generated into the source" test ! -e "$WORK/tree/dashboards/asynq-queue-health.json"
}
refuse_out dashboards "-output dashboards is dashboards/, which holds the source"
refuse_out ./dashboards/ "is dashboards/, which holds the source"
refuse_out "$WORK/tree/dashboards" "is dashboards/, which holds the source" '<the clone>/dashboards'
refuse_out dashboards/external "is dashboards/external/, which holds the source"
refuse_out dashboards/output/.. "is dashboards/, which holds the source"
refuse_out "" "DASHBOARDS_OUT is empty"

echo "# make dashboards-sync and dashboards-upgrade with ONLY"
printf '%s' "$TWO" > "$WORK/tree/dashboards/external/manifest.yaml"
rm -rf "$WORK/served" "$WORK/tmp"
mkdir -p "$WORK/served" "$WORK/tmp"
: > "$WORK/curl.log"
c=0; PATH="$WORK/bin:$PATH" make -s -C "$WORK/tree" dashboards-sync ONLY=pg >/dev/null 2>&1 || c=$?
check "make dashboards-sync ONLY=pg: exit 0" test "$c" = 0
check "make dashboards-sync ONLY=pg: fetches pg alone" test "$(cat "$WORK/curl.log")" = 202
check "make dashboards-sync ONLY=pg: writes pg alone" test -e "$WORK/tree/dashboards/external/pg.json" -a ! -e "$WORK/tree/dashboards/external/pods.json"
: > "$WORK/curl.log"
c=0; PATH="$WORK/bin:$PATH" make -s -C "$WORK/tree" dashboards-upgrade ONLY=pg >/dev/null 2>&1 || c=$?
check "make dashboards-upgrade ONLY=pg: exit 0" test "$c" = 0
check "make dashboards-upgrade ONLY=pg: fetches pg again, alone" test "$(cat "$WORK/curl.log")" = 202

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
