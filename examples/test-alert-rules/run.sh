#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
source scripts/example-setup.sh

make -s lint-alerts test-alerts
