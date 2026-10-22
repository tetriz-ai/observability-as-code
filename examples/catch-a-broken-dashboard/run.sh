#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

tree=$(example_tree)
# The mistake under test: the Goroutines query from add-a-dashboard without its $job filter.
grep -v '^//go:build example$' examples/add-a-dashboard/go_runtime.go |
  sed 's/go_goroutines{[^}]*}/go_goroutines/' > "$tree/dashboards/go_runtime.go"
patch -s -d "$tree" -p1 < examples/add-a-dashboard/register.patch
cd "$tree"
if make -s lint-dashboards > lint-dashboards.log 2>&1; then
  echo "the query without the \$job filter passed the lint" >&2
  exit 1
fi
# Left out: make's own "Error" line, which differs between make versions, the list of rules
# dashboard-linter prints for every file, its colour codes, and the scratch copy's path.
esc=$(printf '\033')
grep -v -e '^make' -e '^Checks that ' lint-dashboards.log |
  sed -e "s/$esc\[[0-9;]*m//g" -e 's#[^ ]*/dashboards/output/#dashboards/output/#'
echo "make lint-dashboards failed: the Goroutines query has no \$job filter"
