#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

tree=$(example_tree)
cp examples/add-an-alert-rule/rules-checkout.yaml "$tree/alerts/"
cp examples/add-an-alert-rule/checkout_alerts_test.yaml "$tree/alerts/tests/"
cd "$tree"
make -s lint-alerts test-alerts
