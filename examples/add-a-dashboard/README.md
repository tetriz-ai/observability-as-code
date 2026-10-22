Adds a dashboard of your own. `go_runtime.go` builds a two-panel Go runtime dashboard from the
shipped helpers, and `register.patch` adds its line to `dashboards()` in `dashboards/main.go`. The
script applies both to a scratch copy, then vets, generates and lints the new dashboard with
`dashboard-linter --strict`, which must be on `PATH`.

```sh
make examples EXAMPLE=add-a-dashboard
```

```text
wrote dashboards/output/asynq-queue-health.json
wrote dashboards/output/redis-command-health.json
wrote dashboards/output/go-runtime.json
no community dashboards in dashboards/external/ (make dashboards-sync fetches them)
dashboard-linter --strict: go-runtime.json passes
```
