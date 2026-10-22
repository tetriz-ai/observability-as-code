Shows `make lint-dashboards` catching a mistake: the Go runtime dashboard from
[add-a-dashboard](../add-a-dashboard/) with the `$job` filter dropped from its Goroutines query,
linted on a scratch copy of the repository. The script exits 0 only because the lint failed, and
needs `dashboard-linter` and `patch` on `PATH`. Its output leaves out the list of rules
dashboard-linter prints for every file, its colour codes, the scratch path and make's own `Error`
line.

```sh
make examples EXAMPLE=catch-a-broken-dashboard
```

```text
wrote dashboards/output/asynq-queue-health.json
wrote dashboards/output/redis-command-health.json
wrote dashboards/output/go-runtime.json
no community dashboards in dashboards/external/ (make dashboards-sync fetches them)
linting dashboards/output/asynq-queue-health.json
linting dashboards/output/go-runtime.json
[❌] Dashboard 'Go Runtime', panel 'Goroutines', target idx '0' invalid PromQL query 'go_goroutines': job selector not found
Error: there were linting errors, please see previous output
there were linting errors, please see previous output
linting dashboards/output/redis-command-health.json
make lint-dashboards failed: the Goroutines query has no $job filter
```
