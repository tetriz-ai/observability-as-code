#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Tests for scripts/install-ci-tools.sh that need no network: with a fake uname on PATH, the
# Linux x86-64 downloads are refused on another platform and nothing is installed; with fake
# curl, sha256sum, tar, git and go, an accepted tool reaches its install step. Also tests that
# make lint-go runs only the golangci-lint version the script pins.
# Usage: scripts/test-install-ci-tools.sh

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
check() { local name=$1; shift; if "$@"; then pass=$((pass + 1)); echo "ok   $name"; else fail=$((fail + 1)); echo "FAIL $name"; fi; }

mkdir -p "$WORK/bin" "$WORK/out"
printf '#!/usr/bin/env bash\necho "Darwin arm64"\n' > "$WORK/bin/uname"
chmod +x "$WORK/bin/uname"

for tool in promtool yq shellcheck; do
  c=0
  PATH="$WORK/bin:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" "$tool" >/dev/null 2>"$WORK/err" || c=$?
  check "$tool on Darwin arm64: exit 2" test "$c" = 2
  check "$tool on Darwin arm64: names the platform and where to get it" grep -q "^$tool: this script installs the Linux x86-64 build, not one for Darwin arm64. Install it from https://" "$WORK/err"
  check "$tool on Darwin arm64: nothing installed" test -z "$(ls -A "$WORK/out")"
done
c=0
PATH="$WORK/bin:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" nonsense >/dev/null 2>&1 || c=$?
check "unknown tool: exit 2" test "$c" = 2

# BIN unset on Darwin: the refusal leaves no ~/.local/bin behind.
mkdir -p "$WORK/home"
c=0
env -u BIN HOME="$WORK/home" PATH="$WORK/bin:$PATH" bash "$ROOT/scripts/install-ci-tools.sh" promtool >/dev/null 2>&1 || c=$?
check "promtool on Darwin, BIN unset: exit 2" test "$c" = 2
check "promtool on Darwin, BIN unset: no ~/.local/bin created" test ! -e "$WORK/home/.local"

# On Linux aarch64 the x86-64 downloads are refused too.
mkdir -p "$WORK/arm"
printf '#!/usr/bin/env bash\necho "Linux aarch64"\n' > "$WORK/arm/uname"
chmod +x "$WORK/arm/uname"
c=0
PATH="$WORK/arm:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" promtool >/dev/null 2>"$WORK/err" || c=$?
check "promtool on Linux aarch64: exit 2" test "$c" = 2
check "promtool on Linux aarch64: names the platform" grep -q 'not one for Linux aarch64' "$WORK/err"

# On Linux x86_64, with a fake curl, sha256sum and tar, promtool reaches its install step.
mkdir -p "$WORK/linux"
printf '#!/usr/bin/env bash\necho "Linux x86_64"\n' > "$WORK/linux/uname"
cat > "$WORK/linux/curl" <<'EOF2'
#!/usr/bin/env bash
while [ $# -gt 0 ]; do [ "$1" = -o ] && { echo fake > "$2"; exit 0; }; shift; done
exit 1
EOF2
printf '#!/usr/bin/env bash\ncat >/dev/null\n' > "$WORK/linux/sha256sum"
cat > "$WORK/linux/tar" <<'EOF2'
#!/usr/bin/env bash
dir=.; member=
while [ $# -gt 0 ]; do case "$1" in -C) dir=$2; shift 2 ;; -*) shift ;; *) member=$1; shift ;; esac; done
mkdir -p "$dir/$(dirname "$member")" && printf '#!/bin/sh\n' > "$dir/$member"
EOF2
chmod +x "$WORK/linux/"*
rm -rf "$WORK/out"; mkdir -p "$WORK/out"
c=0
PATH="$WORK/linux:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" promtool >"$WORK/stdout" 2>&1 || c=$?
check "promtool on Linux x86_64: exit 0" test "$c" = 0
check "promtool on Linux x86_64: installed" test -x "$WORK/out/promtool"
check "promtool on Linux x86_64: says so" grep -qx 'installed promtool' "$WORK/stdout"
c=0
env -u BIN HOME="$WORK/home" PATH="$WORK/linux:$PATH" bash "$ROOT/scripts/install-ci-tools.sh" yq >/dev/null 2>&1 || c=$?
check "yq on Linux x86_64, BIN unset: installed into ~/.local/bin" test "$c" = 0 -a -x "$WORK/home/.local/bin/yq"

# dashboard-linter is built from source on any platform: on Darwin, with a fake git and go, it is
# not refused.
commit=$(sed -n 's/^DASHBOARD_LINTER_COMMIT=//p' "$ROOT/scripts/install-ci-tools.sh")
cat > "$WORK/bin/git" <<EOF2
#!/usr/bin/env bash
case " \$* " in
  *" rev-parse HEAD "*) echo "$commit" ;;
  *" clone "*) mkdir -p "\${*: -1}" ;;
esac
EOF2
cat > "$WORK/bin/go" <<'EOF2'
#!/usr/bin/env bash
while [ $# -gt 0 ]; do [ "$1" = -o ] && { printf '#!/bin/sh\n' > "$2"; chmod +x "$2"; exit 0; }; shift; done
exit 1
EOF2
chmod +x "$WORK/bin/git" "$WORK/bin/go"
rm -rf "$WORK/out"; mkdir -p "$WORK/out"
c=0
PATH="$WORK/bin:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" dashboard-linter >"$WORK/stdout" 2>&1 || c=$?
check "dashboard-linter on Darwin arm64: exit 0" test "$c" = 0
check "dashboard-linter on Darwin arm64: installed" test -x "$WORK/out/dashboard-linter"

# golangci-lint has macOS downloads too: on Darwin arm64, with the fake curl, sha256sum and tar,
# it reaches its install step; on Linux aarch64 it is refused.
mkdir -p "$WORK/mac"
cp "$WORK/linux/curl" "$WORK/linux/sha256sum" "$WORK/linux/tar" "$WORK/mac/"
printf '#!/usr/bin/env bash\necho "Darwin arm64"\n' > "$WORK/mac/uname"
chmod +x "$WORK/mac/"*
rm -rf "$WORK/out"; mkdir -p "$WORK/out"
c=0
PATH="$WORK/mac:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" golangci-lint >"$WORK/stdout" 2>&1 || c=$?
check "golangci-lint on Darwin arm64: exit 0" test "$c" = 0
check "golangci-lint on Darwin arm64: installed" test -x "$WORK/out/golangci-lint"
c=0
PATH="$WORK/linux:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" golangci-lint >/dev/null 2>&1 || c=$?
check "golangci-lint on Linux x86_64: exit 0" test "$c" = 0
rm -rf "$WORK/out"; mkdir -p "$WORK/out"
c=0
PATH="$WORK/arm:$PATH" BIN="$WORK/out" bash "$ROOT/scripts/install-ci-tools.sh" golangci-lint >/dev/null 2>"$WORK/err" || c=$?
check "golangci-lint on Linux aarch64: exit 2" test "$c" = 2
check "golangci-lint on Linux aarch64: names the platform and where to get it" grep -q 'not one for Linux aarch64. Install it from https://github.com/golangci/golangci-lint/releases' "$WORK/err"
check "golangci-lint on Linux aarch64: nothing installed" test -z "$(ls -A "$WORK/out")"

# make lint-go runs only the golangci-lint version the installer pins: another version on PATH is
# refused with the install command, and the pinned one is run.
want=$(sed -n 's/^GOLANGCI_LINT_VERSION=v//p' "$ROOT/scripts/install-ci-tools.sh")
mkdir -p "$WORK/lint"
cat > "$WORK/lint/golangci-lint" <<'EOF2'
#!/usr/bin/env bash
if [ "$1" = version ]; then echo "$FAKE_VERSION"; else echo "ran: $*"; fi
EOF2
chmod +x "$WORK/lint/golangci-lint"
c=0
FAKE_VERSION=2.11.4 PATH="$WORK/lint:$PATH" make -s --no-print-directory -C "$ROOT" lint-go >"$WORK/stdout" 2>"$WORK/err" || c=$?
check "make lint-go with golangci-lint 2.11.4: fails" test "$c" != 0
check "make lint-go with golangci-lint 2.11.4: names both versions and the fix" grep -q "golangci-lint 2.11.4 found, CI pins $want: run make install-tools TOOLS=golangci-lint" "$WORK/err"
check "make lint-go with golangci-lint 2.11.4: does not lint" test ! -s "$WORK/stdout"
c=0
FAKE_VERSION=$want PATH="$WORK/lint:$PATH" make -s --no-print-directory -C "$ROOT" lint-go >"$WORK/stdout" 2>&1 || c=$?
check "make lint-go with golangci-lint $want: runs it" test "$c" = 0 -a "$(cat "$WORK/stdout")" = "ran: run"

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
