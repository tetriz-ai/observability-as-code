Generates every dashboard in `dashboards/` as Grafana JSON with `make dashboards`, the way you run
it after editing a dashboard's Go source. It writes into `dashboards/output/`, which
`make dev-monitoring` mounts into Grafana and git ignores. The last line says the community dashboards are not fetched yet:
`make dashboards-sync` adds them.

```sh
make examples EXAMPLE=generate-dashboards
```

```text
wrote dashboards/output/asynq-queue-health.json
wrote dashboards/output/redis-command-health.json
no community dashboards in dashboards/external/ (make dashboards-sync fetches them)
```
