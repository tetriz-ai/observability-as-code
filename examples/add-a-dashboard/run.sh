#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

tree=$(example_tree)
grep -v '^//go:build example$' examples/add-a-dashboard/go_runtime.go > "$tree/dashboards/go_runtime.go"
patch -s -d "$tree" -p1 < examples/add-a-dashboard/register.patch
cd "$tree"
go vet ./dashboards
go run ./dashboards -output dashboards/output
dashboard-linter lint --strict -c .dashboard-linter.yaml dashboards/output/go-runtime.json > /dev/null
echo "dashboard-linter --strict: go-runtime.json passes"
