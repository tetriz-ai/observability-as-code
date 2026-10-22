#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

tree=$(example_tree)
# Start from your own rules: the example alerts use the default labels.
rm "$tree"/alerts/rules-*.yaml "$tree"/alerts/tests/*.yaml
cp examples/use-your-own-alert-labels/rules-payments.yaml "$tree/alerts/"
cp examples/use-your-own-alert-labels/payments_alerts_test.yaml "$tree/alerts/tests/"
cd "$tree"
echo "== under the defaults (severity p0 p1 p2, routing label team)"
if make -s lint-alerts > lint.log 2>&1; then
  echo "the defaults accepted critical and owner" >&2
  exit 1
fi
# make's own "Error 1" line differs between make versions, so it is left out.
grep -v '^make' lint.log
echo "== with your own labels"
make -s lint-alerts test-alerts ALERT_SEVERITIES="critical warning" ALERT_ROUTING_LABEL=owner
