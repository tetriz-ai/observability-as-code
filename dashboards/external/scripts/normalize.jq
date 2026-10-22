# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Normalizes a dashboard fetched from grafana.com; sync.sh passes the manifest uid as $uid.
#   - del(.id): an id would overwrite whichever dashboard holds that id on upload.
#   - .uid = $uid: a stable uid makes repeated uploads idempotent.
#   - one datasource template variable per __inputs datasource the dashboard references outside
#     __inputs (a panel, a variable, an annotation) and does not define: file provisioning and
#     the dashboard API do not resolve __inputs, so a panel pointing at ${DS_PROMETHEUS} would
#     otherwise find no datasource. A reference is any of Grafana's forms: ${NAME}, ${NAME:fmt},
#     $NAME and [[NAME]]. An input nothing references gets no variable, which would only add a
#     dropdown that changes nothing.
del(.id)
| .uid = $uid
| ([.templating.list[]?.name]) as $defined
| ([del(.__inputs) | .. | strings]) as $strings
| [.__inputs[]? | select(.type == "datasource") | select(.name as $n | any($defined[]; . == $n) | not)
   | select(("\\$\\{\(.name)(:[a-z]+)?\\}|\\$\(.name)\\b|\\[\\[\(.name)\\]\\]") as $re
       | any($strings[]; test($re)))
   | {name: .name, label: (.label // .name), type: "datasource", query: .pluginId,
      current: {}, hide: 0, options: [], refresh: 1, regex: "", skipUrlSync: false}] as $add
| if ($add | length) > 0 then .templating.list = ((.templating.list // []) + $add) else . end
