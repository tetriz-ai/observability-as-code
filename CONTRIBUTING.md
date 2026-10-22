# Contributing

## Build and test

The Go floor is 1.23. CI runs `make check` on 1.23, 1.26 and 1.27.

```sh
make install-tools
make dashboards
make check
```

`make check` runs CI's main job: `make lint-go`, the dashboard and alert lints, `make test` and
`make examples`. `make test` runs the Go tests, the alert rule tests and the shell tests of the
alert and dashboard gates, the sync script, the metric-name extraction, the CI installer and the
Docker targets. `make help` lists every target.

`make check` needs promtool, yq v4, jq, dashboard-linter and golangci-lint on `PATH`, and
`make examples` also needs `shellcheck` and `patch`. Without promtool the Go tests fail. Set
`OAC_SKIP_PROMTOOL=1` to skip their PromQL checks on purpose.

| Tool | Version CI pins |
|---|---|
| promtool | 3.14.0 |
| yq | v4.53.6 |
| shellcheck | v0.11.0 |
| dashboard-linter | v0.3.0, built from its tag with Go 1.26.3 or newer |
| golangci-lint | v2.14.0 |
| govulncheck | v1.9.0 |

`make install-tools` installs the first five into `~/.local/bin`, or into `BIN`. promtool, yq and
shellcheck are Linux x86-64 downloads, so on macOS install them with Homebrew and run
`make install-tools TOOLS="dashboard-linter golangci-lint"`, whose linter build needs Go 1.26.3 or
newer. golangci-lint installs with `make install-tools TOOLS=golangci-lint`.

`make lint-go` lints with golangci-lint v2.14.0 and the checked-in `.golangci.yml`. A rule that
must be off is turned off in that file with a comment, never with an inline directive.

`make acceptance`, `make check-dev-stack` and `make vulncheck` run CI's other jobs: the acceptance
test against an empty Grafana in Docker, the local preview stack, and govulncheck.

A bare `go build ./...` fails, because it writes a binary over `dashboards/`. `make dashboards`
builds.

## Changes

- **Every pull request needs a test.** A change to an alert rule changes or adds a case in
  `alerts/tests/`, and a behaviour change in Go or in a script adds a test that fails before it and
  passes after.
- **Dashboards** change in Go under `dashboards/`, never in generated JSON.
- **Commit messages** start with a short summary line in the imperative, such as "Add a backlog
  panel to the asynq dashboard".
- **The changelog** gets a line under `[Unreleased]` for any change a user would notice.

## Licensing of contributions

No sign-off line and no separately signed agreement are needed. By opening a pull request you
agree that your contribution is released under the project's [LICENSE](LICENSE).
