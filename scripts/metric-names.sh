#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Prints the metric names that PromQL expressions read, one per line and sorted, for
# scripts/acceptance.sh to check against ALLOWED. Reads one expression per line on standard input.
#
# Strings, label matchers, range selectors, grouping clauses, offsets and numbers are stripped
# from each expression. What is left that is not a function call or a keyword is a metric name.
# A metric named inside a matcher, as in {__name__="x"} or {"x"}, would be stripped with the
# matcher and never checked, so such an expression exits 1 with
# "name the metric outside the matcher: <expr>" before any name is printed.
#
# Usage: scripts/metric-names.sh < expressions

set -euo pipefail

exprs=$(cat)

inside=$(grep -E '__name__|\{[[:space:]]*"' <<<"$exprs" || true)
if [ -n "$inside" ]; then
  while IFS= read -r expr; do
    echo "name the metric outside the matcher: $expr" >&2
  done <<<"$inside"
  exit 1
fi

# The last sed expression drops numeric literals (1e3, 2.5E+3, .5, 0x10), so the exponent of a
# number is not read as a name. Its leading class keeps the digits inside a name or a function
# such as log10 in place.
printf '%s\n' "$exprs" |
  sed -E -e 's/"([^"\\]|\\.)*"//g' -e 's/\{[^}]*\}//g' -e 's/\[[^]]*\]//g' \
         -e 's/(by|without|on|ignoring|group_left|group_right)[[:space:]]*\([^)]*\)//g' \
         -e 's/offset[[:space:]]+[0-9a-z]+//g' \
         -e 's/(^|[^A-Za-z0-9_:.])(0[xX][0-9A-Fa-f]+|[0-9]*\.?[0-9]+([eE][-+]?[0-9]+)?)/\1/g' |
  grep -oE '[A-Za-z_:][A-Za-z0-9_:]*[[:space:]]*\(?' | grep -vE '\($' | tr -d ' ' |
  grep -vixE 'and|or|unless|bool|atan2|inf|nan' | sort -u || true
