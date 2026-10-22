#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Installs the pinned command-line tools CI uses into $BIN (default ~/.local/bin, which must be on
# PATH), verifying each download's SHA-256. promtool, yq and shellcheck are Linux x86-64
# downloads, the CI runner's platform; golangci-lint has Linux x86-64 and macOS downloads;
# dashboard-linter is built from source and works anywhere. make install-tools runs it.
# Usage: scripts/install-ci-tools.sh <tool>...
#   tools: promtool yq shellcheck dashboard-linter golangci-lint
# dashboard-linter is built from source (its go.mod has replace directives, so `go install`
# refuses it) and needs Go 1.26.3 or newer on PATH.

set -euo pipefail

BIN=${BIN:-$HOME/.local/bin}
PROMETHEUS_VERSION=3.14.0
PROMETHEUS_SHA256=f665c6da19eb7ba399c915d30c7d9793c9b417bf8a749b504bc470678631478d
YQ_VERSION=v4.53.6
YQ_SHA256=c5f056448f973ae7d39b5401949648a78f2dc1947d6a8eb65be60d5c504b9385
SHELLCHECK_VERSION=v0.11.0
SHELLCHECK_SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
DASHBOARD_LINTER_VERSION=v0.3.0
DASHBOARD_LINTER_COMMIT=606bb30042fc986e3f77de412096d09d3b031b65
# From the release's checksums.txt.
GOLANGCI_LINT_VERSION=v2.14.0
GOLANGCI_LINT_SHA256_LINUX_AMD64=ab90aeb7b066f92a33415b638a50fe5344bbb75a0d32ad30cc248d88f81032ab
GOLANGCI_LINT_SHA256_DARWIN_ARM64=5ef5f36a7147e91dc58ef9ef4d11bb7bad5ead0c76eb6c01327a73c641d1dcc3
GOLANGCI_LINT_SHA256_DARWIN_AMD64=a5667c1c3536be1740133213e1e822bfb8f0d98ea12903174d6d5f635e4ed68d

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# promtool, yq and shellcheck come as Linux x86-64 downloads; anywhere else they would install a
# binary that cannot run, so they are refused with the page to get the right build from.
platform=$(uname -sm)
linux_only() { # linux_only <tool> <release page>
  if [ "$platform" != "Linux x86_64" ]; then
    echo "$1: this script installs the Linux x86-64 build, not one for $platform. Install it from $2" >&2
    exit 2
  fi
}

fetch() { # fetch <url> <sha256> <file>
  curl -sSfL -o "$3" "$1"
  echo "$2  $3" | sha256sum -c --quiet -
}

# $BIN is created only once a tool is past its platform check, so a refusal leaves nothing behind.
install_bin() { # install_bin <file> <name>
  mkdir -p "$BIN"
  install -m 0755 "$1" "$BIN/$2"
}

for tool in "$@"; do
  case "$tool" in
    promtool)
      linux_only promtool https://prometheus.io/download/
      f="$WORK/prometheus.tar.gz"
      fetch "https://github.com/prometheus/prometheus/releases/download/v$PROMETHEUS_VERSION/prometheus-$PROMETHEUS_VERSION.linux-amd64.tar.gz" "$PROMETHEUS_SHA256" "$f"
      tar -xzf "$f" -C "$WORK" "prometheus-$PROMETHEUS_VERSION.linux-amd64/promtool"
      install_bin "$WORK/prometheus-$PROMETHEUS_VERSION.linux-amd64/promtool" promtool
      ;;
    yq)
      linux_only yq https://github.com/mikefarah/yq/releases
      fetch "https://github.com/mikefarah/yq/releases/download/$YQ_VERSION/yq_linux_amd64" "$YQ_SHA256" "$WORK/yq"
      install_bin "$WORK/yq" yq
      ;;
    shellcheck)
      linux_only shellcheck https://github.com/koalaman/shellcheck/releases
      f="$WORK/shellcheck.tar.xz"
      fetch "https://github.com/koalaman/shellcheck/releases/download/$SHELLCHECK_VERSION/shellcheck-$SHELLCHECK_VERSION.linux.x86_64.tar.xz" "$SHELLCHECK_SHA256" "$f"
      tar -xJf "$f" -C "$WORK"
      install_bin "$WORK/shellcheck-$SHELLCHECK_VERSION/shellcheck" shellcheck
      ;;
    dashboard-linter)
      git -c advice.detachedHead=false clone -q --depth 1 --branch "$DASHBOARD_LINTER_VERSION" https://github.com/grafana/dashboard-linter "$WORK/dl"
      got=$(git -C "$WORK/dl" rev-parse HEAD)
      [ "$got" = "$DASHBOARD_LINTER_COMMIT" ] || { echo "dashboard-linter $DASHBOARD_LINTER_VERSION is $got, want $DASHBOARD_LINTER_COMMIT" >&2; exit 1; }
      (cd "$WORK/dl" && go build -o "$WORK/dashboard-linter" .)
      install_bin "$WORK/dashboard-linter" dashboard-linter
      ;;
    golangci-lint)
      case "$platform" in
        "Linux x86_64") asset=linux-amd64 sum=$GOLANGCI_LINT_SHA256_LINUX_AMD64 ;;
        "Darwin arm64") asset=darwin-arm64 sum=$GOLANGCI_LINT_SHA256_DARWIN_ARM64 ;;
        "Darwin x86_64") asset=darwin-amd64 sum=$GOLANGCI_LINT_SHA256_DARWIN_AMD64 ;;
        *)
          echo "golangci-lint: this script installs the Linux x86-64 and macOS builds, not one for $platform. Install it from https://github.com/golangci/golangci-lint/releases" >&2
          exit 2
          ;;
      esac
      d="golangci-lint-${GOLANGCI_LINT_VERSION#v}-$asset"
      f="$WORK/golangci-lint.tar.gz"
      fetch "https://github.com/golangci/golangci-lint/releases/download/$GOLANGCI_LINT_VERSION/$d.tar.gz" "$sum" "$f"
      tar -xzf "$f" -C "$WORK" "$d/golangci-lint"
      install_bin "$WORK/$d/golangci-lint" golangci-lint
      ;;
    *)
      echo "unknown tool: $tool" >&2
      exit 2
      ;;
  esac
  echo "installed $tool"
done
