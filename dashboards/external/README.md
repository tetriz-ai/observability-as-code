# Community dashboards

This folder holds community dashboards from grafana.com, fetched as JSON. `manifest.yaml` lists
them, and `make dashboards-sync` fetches them with `scripts/sync.sh`. The generator in `../main.go` copies each
JSON into `<output>/external/` beside the dashboards built from Go.

This repository ships the manifest and the script, not the fetched JSON: each community
dashboard is published under its author's own terms on grafana.com.

## Dashboards the manifest lists

| File                          | Source                                    | What it covers                                                                |
|-------------------------------|-------------------------------------------|-------------------------------------------------------------------------------|
| `k8s-persistent-volumes.json` | grafana.com/dashboards/13646              | PVC capacity and usage, with a 4-hour linear forecast.                        |
| `k8s-views-pods.json`         | grafana.com/dashboards/15760              | CPU, memory, network and restarts for each pod, from kube-prometheus-stack metrics. |
| `k8s-views-global.json`       | grafana.com/dashboards/15757              | Resources across the cluster by namespace and workload.                       |
| `postgresql-database.json`    | grafana.com/dashboards/9628               | PostgreSQL internals from postgres-exporter.                                  |

## Required metrics

All four read standard exporter metrics: `kubelet_volume_stats_*`, `kube_*`
(kube-state-metrics), `container_*` (cAdvisor), `pg_*` (postgres-exporter). No custom exporter
is required.

## Adding a new dashboard

1. Find it on grafana.com, note its ID.
2. Append an entry to [`manifest.yaml`](manifest.yaml): `{name, grafana_com_id, uid}`. Both the ID and an optional `revision` must be
   positive integers. Set `revision` to the number on grafana.com for a repeatable download;
   without it, sync fetches `latest`.
3. Run:
   ```bash
   make dashboards-sync
   ```
   This fetches only entries that don't yet have a JSON file.
4. Run `make dashboards` to check that the file has a string uid and title. `make lint-dashboards`
   lints only the Go-built dashboards.
5. Run `make dev-monitoring` to preview it.
6. Commit the manifest change, and commit the JSON only if its grafana.com terms allow.

## Upgrading (refresh all)

```bash
make dashboards-upgrade
```

`make dashboards-upgrade` fetches every manifest entry again and overwrites its JSON. Entries
with `revision` keep that revision until you edit the manifest; unpinned entries fetch `latest`.
Review the change with `git status dashboards/external/` and `git diff` before committing, and
commit the JSON only if its grafana.com terms allow. Run it monthly, or when an upstream has
published fixes you want.

Refresh a single dashboard:

```bash
make dashboards-upgrade ONLY=k8s-views-pods
```

## Removing

1. Delete the entry from `manifest.yaml`.
2. Delete `dashboards/external/<name>.json`.
3. The next `sync` run warns about any JSON left without a manifest entry.
4. Delete the dashboard from Grafana. Grafana's file provisioning removes a dashboard whose file
   disappears. A dashboard uploaded by hand stays until you delete it.

## What the sync script normalizes

`scripts/normalize.jq` changes three things and leaves `__elements`, `gnetId`, title prefixes and
the `editable` flag as fetched.

- **`id`** is dropped, so an upload cannot overwrite whichever dashboard holds that id.
- **`uid`** is set from the manifest, so repeated uploads are idempotent.
- **`__inputs` datasources** the dashboard references (`${<name>}`, `${<name>:<format>}`,
  `$<name>` or `[[<name>]]`) but does not define become datasource variables, because
  provisioning does not resolve `__inputs`. One it never references gets no variable, which
  would only add a dropdown that changes nothing.

Before fetching anything, `scripts/check-manifest.jq` rejects an entry without a name, uid or
a positive integer grafana_com_id, a non-integer or non-positive revision, a name that would leave this folder or starts with `.`, `_` or `-`, and a uid
Grafana refuses.
