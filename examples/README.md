# Examples

| You want | Example |
|---|---|
| Generate the dashboards as Grafana JSON | [generate-dashboards](generate-dashboards/) |
| Run the alert gates CI runs | [test-alert-rules](test-alert-rules/) |
| See a test catch a mistyped threshold | [catch-a-broken-alert](catch-a-broken-alert/) |
| See the lint catch a query without the `$job` filter | [catch-a-broken-dashboard](catch-a-broken-dashboard/) |
| Route alerts on your own labels | [use-your-own-alert-labels](use-your-own-alert-labels/) |
| Add an alert rule with its unit test | [add-an-alert-rule](add-an-alert-rule/) |
| Add a dashboard written in Go | [add-a-dashboard](add-a-dashboard/) |

`make examples` runs every example and compares its standard output with the last block of its
README, ignoring spaces at line ends. `make examples EXAMPLE=<name>` runs one and prints its
output.

Examples run offline from the Go module cache, with an empty `HOME` and HTTP proxies pointed at a
closed port. The ones that add or change files work on a scratch copy of the repository. The
runner removes `dashboards/output/` afterwards only if the run created it.
