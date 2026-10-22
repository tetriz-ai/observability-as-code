#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for the Docker-backed targets that need no Docker: scripts/check-dev-stack.sh, which
# make check-dev-stack runs, against a fake make and a fake Grafana API; and the teardown of
# make acceptance, with a fake docker and a stub acceptance script.
# Usage: scripts/test-docker-targets.sh   (needs bash, jq, make)

set -euo pipefail

for bin in jq make; do
  command -v "$bin" >/dev/null 2>&1 || { echo "scripts/test-docker-targets.sh needs $bin on PATH" >&2; exit 2; }
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
export WORK

pass=0
fail=0
check() { local name=$1; shift; if "$@"; then pass=$((pass + 1)); echo "ok   $name"; else fail=$((fail + 1)); echo "FAIL $name"; fi; }

echo "# check-dev-stack checks every dashboard, the community ones in external/ included"
mkdir -p "$WORK/dev/scripts" "$WORK/dev/out/external" "$WORK/devbin"
cp "$ROOT/scripts/check-dev-stack.sh" "$WORK/dev/scripts/"
echo '{"uid": "root-dash", "title": "Root"}' > "$WORK/dev/out/root.json"
echo '{"uid": "community-dash", "title": "Community"}' > "$WORK/dev/out/external/community.json"
# The stack is taken as up (fake make), retries do not wait (fake sleep), and the fake curl answers
# for the dashboards named in SERVED and for a healthy datasource.
printf '#!/usr/bin/env bash\nexit 0\n' > "$WORK/devbin/make"
printf '#!/usr/bin/env bash\nexit 0\n' > "$WORK/devbin/sleep"
cat > "$WORK/devbin/curl" <<'EOF'
#!/usr/bin/env bash
url="${*: -1}"
case "$url" in
  */api/datasources/uid/prometheus/health) echo '{"status": "OK"}' ;;
  */api/dashboards/uid/*)
    uid=${url##*/}
    for s in $SERVED; do
      if [ "$s" = "$uid" ]; then echo "{\"dashboard\": {\"title\": \"$uid\"}}"; exit 0; fi
    done
    exit 22 ;;
  *) exit 22 ;;
esac
EOF
chmod +x "$WORK/devbin/"*
dev() { # dev <served uids>: the exit status of check-dev-stack.sh
  local c=0
  SERVED="$1" DASHBOARDS_OUT="$WORK/dev/out" PATH="$WORK/devbin:$PATH" \
    bash "$WORK/dev/scripts/check-dev-stack.sh" >"$WORK/out" 2>"$WORK/err" || c=$?
  echo "$c"
}
check "every dashboard provisioned: exit 0" test "$(dev "root-dash community-dash")" = 0
check "every dashboard provisioned: PASS" grep -qx 'dev stack: PASS' "$WORK/out"
check "every dashboard provisioned: the community one checked" grep -q 'provisioned community-dash' "$WORK/out"
check "a community dashboard not provisioned: exit 1" test "$(dev "root-dash")" = 1
check "a community dashboard not provisioned: says which" grep -q 'community-dash was not provisioned' "$WORK/err"

echo "# make acceptance tears its Grafana down whatever fails"
mkdir -p "$WORK/acc/scripts" "$WORK/accbin"
cp "$ROOT/Makefile" "$WORK/acc/"
cat > "$WORK/acc/scripts/acceptance.sh" <<'EOF'
#!/usr/bin/env bash
echo checks >> "$WORK/docker.log"
exit "${FAKE_CHECKS_EXIT:-0}"
EOF
cat > "$WORK/accbin/docker" <<'EOF'
#!/usr/bin/env bash
case " $* " in
  *" up "*) echo up >> "$WORK/docker.log"; exit "${FAKE_UP_EXIT:-0}" ;;
  *" down "*) echo down >> "$WORK/docker.log" ;;
esac
EOF
chmod +x "$WORK/accbin/docker"
acc() { # acc: the exit status of make acceptance; the docker calls and checks in $WORK/docker.log
  local c=0
  : > "$WORK/docker.log"
  PATH="$WORK/accbin:$PATH" make -s --no-print-directory -C "$WORK/acc" acceptance >/dev/null 2>"$WORK/err" || c=$?
  echo "$c"
}
log() { paste -sd' ' "$WORK/docker.log"; }
c=$(FAKE_UP_EXIT=19 acc)
check "startup fails: make fails" test "$c" != 0
check "startup fails: with the startup's status, 19" grep -q 'Error 19' "$WORK/err"
check "startup fails: the checks do not run, and down is attempted" test "$(log)" = "up down"
c=$(FAKE_CHECKS_EXIT=1 acc)
check "the checks fail: make fails with their status" test "$c" != 0 -a -n "$(grep 'Error 1$' "$WORK/err")"
check "the checks fail: down is attempted" test "$(log)" = "up checks down"
c=$(acc)
check "everything passes: exit 0" test "$c" = 0
check "everything passes: down after the checks" test "$(log)" = "up checks down"

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
