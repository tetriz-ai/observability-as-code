# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Prints one line per manifest entry sync.sh must reject; prints nothing for a good manifest.
# A name becomes a file name, so it may not leave external/. Nor may it start with "." or "_",
# which Go tooling (and, for ".", shell globs) treats as a hidden file, or "-", which grep and mv
# read as an option. Grafana rejects a uid over 40 characters or with other characters; the id is
# what sync.sh fetches.
(.dashboards // []) | to_entries[]
| ((.value.name // "") | tostring) as $n
| ((.value.uid // "") | tostring) as $u
| "manifest entry \(.key + 1) (name: \"\($n)\", uid: \"\($u)\") " as $who
| (select($n == "" or $u == "") | $who + "needs both a name and a uid"),
  (select($n != "" and (($n | test("^[A-Za-z0-9._-]+$") | not) or $n == "." or $n == "..")) | $who + "has a name other than letters, digits, '.', '_' and '-'"),
  (select($n != "" and ($n | test("^[-._]"))) | $who + "has a name starting with '.', '_' or '-'"),
  (select($u != "" and ($u | test("^[A-Za-z0-9_-]{1,40}$") | not)) | $who + "has a uid Grafana rejects: use 1 to 40 letters, digits, '_' or '-'"),
  (select(.value.grafana_com_id | if type == "number" then . <= 0 or . != floor else true end)
    | $who + "needs a positive integer grafana_com_id"),
  (select(.value.revision != null and (.value.revision | if type == "number" then . <= 0 or . != floor else true end))
    | $who + "needs a positive integer revision when set")
