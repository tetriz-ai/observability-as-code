#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

tree=$(example_tree)
cp examples/add-an-alert-rule/checkout_alerts_test.yaml "$tree/alerts/tests/"
# The mistake under test: the 5% threshold typed as 0.5.
sed 's/> 0.05/> 0.5/' examples/add-an-alert-rule/rules-checkout.yaml > "$tree/alerts/rules-checkout.yaml"
cd "$tree"
if make -s test-alerts > test-alerts.log 2>&1; then
  echo "the mistyped threshold passed its tests" >&2
  exit 1
fi
# make's own "Error 1" line differs between make versions, so it is left out.
grep -v '^make' test-alerts.log
echo "make test-alerts failed: the case that should fire at 10% errors got no alert"
