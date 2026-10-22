#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Sourced by every examples/<name>/run.sh. An example runs offline: the Go toolchain on PATH is
# used as is, modules come from the local cache only (scripts/run-examples.sh downloads them
# first), HOME is an empty scratch directory, and HTTP(S) proxies point at a closed port.

set -euo pipefail

EXAMPLES_REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
GOMODCACHE=${GOMODCACHE:-$(go env GOMODCACHE)}
GOCACHE=${GOCACHE:-$(go env GOCACHE)}
export GOMODCACHE GOCACHE GOPROXY=off GOTOOLCHAIN=local

EXAMPLE_SCRATCH=$(mktemp -d)
trap 'rm -rf "$EXAMPLE_SCRATCH"' EXIT
export HOME="$EXAMPLE_SCRATCH/home"
mkdir -p "$HOME"

unset NO_PROXY no_proxy
export HTTP_PROXY=http://127.0.0.1:9 HTTPS_PROXY=http://127.0.0.1:9
export http_proxy=$HTTP_PROXY https_proxy=$HTTPS_PROXY

# example_tree copies what a clone of this repository holds into a scratch directory and prints
# its path, so an example can add files without touching the checkout.
example_tree() {
  local tree="$EXAMPLE_SCRATCH/tree"
  mkdir -p "$tree"
  cp -R "$EXAMPLES_REPO"/{Makefile,go.mod,go.sum,.dashboard-linter.yaml,alerts,dashboards} "$tree"/
  rm -rf "$tree/dashboards/output" "$tree/alerts/.extracted"
  echo "$tree"
}
