#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Reconcile external/*.json with external/manifest.yaml.
#
# Default behavior: fetch only entries that don't yet have a JSON file.
# Use this after editing manifest.yaml to add a new dashboard.
#
#   --refresh           re-fetch every entry, overwriting existing JSON
#                       (used to upgrade to the latest grafana.com revision)
#   --only <name>       operate on a single manifest entry by name
#
# Normalization is bare minimum, in scripts/normalize.jq: drop .id, pin .uid, and declare a
# datasource variable for each __inputs datasource the dashboard uses but does not define.
# __elements / gnetId / title prefixes / editable flag are left as fetched.
#
# Requires: curl, jq, yq.

set -euo pipefail

cd "$(dirname "$0")/.."  # dashboards/external/

MANIFEST="manifest.yaml"
REFRESH=0
ONLY=""

usage() {
  echo "Usage: $0 [--refresh] [--only <name>]" >&2
  exit 2
}

show_help() {
  echo "Usage: $0 [--refresh] [--only <name>]"
  echo "Fetches each dashboard in manifest.yaml that has no JSON yet; --refresh fetches every one again."
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --refresh) REFRESH=1; shift ;;
    --only)    ONLY="${2:-}"; [[ -z "$ONLY" ]] && usage; shift 2 ;;
    -h|--help) show_help ;;
    *)         usage ;;
  esac
done

for bin in curl jq yq; do
  command -v "$bin" >/dev/null 2>&1 || { echo "missing required binary: $bin" >&2; exit 2; }
done

if [[ ! -f "$MANIFEST" ]]; then
  echo "manifest not found: $(pwd)/$MANIFEST" >&2
  exit 1
fi

# A bad manifest is rejected before anything is fetched or written: scripts/check-manifest.jq
# names each entry without a name, uid or grafana_com_id, a name that would leave this folder,
# and a uid Grafana refuses. Two entries may share neither a name (one file) nor a uid (Grafana
# keeps one dashboard per uid).
problems=$(yq -o=json '.' "$MANIFEST" | jq -r -f scripts/check-manifest.jq)
if [[ -n "$problems" ]]; then
  echo "$problems" >&2
  exit 1
fi
for field in name uid; do
  dups=$(yq -r ".dashboards[]?.$field" "$MANIFEST" | sort | uniq -d)
  if [[ -n "$dups" ]]; then
    while IFS= read -r d; do
      echo "$field \"$d\" is used by more than one manifest entry: $(D="$d" yq -r "[.dashboards[] | select(.$field == strenv(D)) | .name + \" (grafana.com/\" + (.grafana_com_id | tostring) + \")\"] | join(\", \")" "$MANIFEST")" >&2
    done <<<"$dups"
    exit 1
  fi
done

# Parse manifest into tab-separated (name, id, uid, revision) tuples.
entries=$(yq -r '.dashboards[] | [.name, .grafana_com_id, .uid, (.revision // "latest")] | @tsv' "$MANIFEST")
if [[ -z "$entries" ]]; then
  echo "manifest has no dashboards entries" >&2
  exit 1
fi

# Warn about orphan JSONs (present on disk, absent from manifest).
expected_names=$(yq -r '.dashboards[].name' "$MANIFEST" | sort -u)
shopt -s nullglob
for f in *.json; do
  existing_name="${f%.json}"
  if ! grep -qxF -- "$existing_name" <<<"$expected_names"; then
    echo "⚠ orphan: $f exists but is not in manifest.yaml — delete one or the other" >&2
  fi
done

tmp=""
trap 'if [[ -n "$tmp" ]]; then rm -f "$tmp"; fi' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

failed=0
matched=0
while IFS=$'\t' read -r name id uid revision; do
  [[ -n "$ONLY" && "$name" != "$ONLY" ]] && continue
  matched=$((matched + 1))
  target="${name}.json"

  if [[ -f "$target" && $REFRESH -eq 0 ]]; then
    echo "• $name: exists, skipping (use --refresh to re-fetch)"
    continue
  fi

  echo "↻ $name  (grafana.com/$id → uid=$uid)"
  tmp=$(mktemp)
  # A 200 whose body is not exactly one object with a string title (empty, null, two JSON values)
  # is a failed fetch too: jq would pass it through, and the file would break the generator later.
  if ! curl -sSf "https://grafana.com/api/dashboards/${id}/revisions/${revision}/download" |
        jq --arg uid "$uid" -f scripts/normalize.jq > "$tmp" ||
     ! jq -s -e 'length == 1 and (.[0] | type == "object" and (.title | type) == "string")' "$tmp" >/dev/null 2>&1; then
    echo "  ✗ fetch or normalize failed" >&2
    rm -f "$tmp"
    tmp=""
    failed=1
    continue
  fi
  mv -- "$tmp" "$target"
  tmp=""
  echo "  ✓ wrote $target"
done <<<"$entries"

if [[ -n "$ONLY" && $matched -eq 0 ]]; then
  echo "no manifest entry matched --only '$ONLY'" >&2
  exit 1
fi

echo ""
echo "review:   git status dashboards/external/"
echo "validate: make dashboards"
exit $failed
