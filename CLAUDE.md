# Guide for coding agents

## What this is

A template repository: Grafana dashboards written as Go builders that `make dashboards` turns
into JSON, and Prometheus alert rules that promtool lints and unit-tests. It is not an importable
Go library, it deploys nothing, and it does not instrument a service. `make` runs everything.

## Layout

- `dashboards/` is package `main`: the generator (`main.go`, the `dashboards()` list), its output
  checks and `.generated` list (`output.go`), shared panel helpers (`helpers.go`), one file per
  example dashboard (`asynq.go`, `redis.go`), Go tests.
- `dashboards/external/` holds the community-dashboard manifest and `scripts/` (sync.sh and its
  two jq programs). Fetched JSON lands here and is not committed in this repository.
- `alerts/` holds PrometheusRule files (`rules-*.yaml`). `alerts/tests/` holds their promtool tests.
- `examples/` holds the runnable examples, `run.sh` scripts indexed in `examples/README.md`.
- `scripts/` holds the shell tests, the examples runner, the acceptance test and the CI installer.

## Commands

The Go floor is 1.23. CI runs `make check` on 1.23, 1.26 and 1.27.

```sh
make install-tools
make dashboards
make check
```

- `make check` is CI's main job: `make lint-go`, the dashboard and alert lints, `make test` and
  `make examples`. It needs `jq` 1.6 or newer, promtool, yq v4, `dashboard-linter` and
  golangci-lint, and the examples `shellcheck` and `patch`. `make acceptance` and
  `make check-dev-stack` need Docker.
- `make install-tools` installs the pinned tools into `~/.local/bin`. promtool, yq and shellcheck
  are Linux x86-64 downloads. On macOS use Homebrew for them and
  `TOOLS="dashboard-linter golangci-lint"`. golangci-lint installs with
  `make install-tools TOOLS=golangci-lint`. `make help` lists every target.
- Without promtool `go test` fails. `OAC_SKIP_PROMTOOL=1` skips its PromQL tests on purpose.

## Contract to keep

- `run` exits 0 on success or `-h`, 1 on a build or write failure or a repeated uid, 2 on a flag
  error, a positional argument, a source directory, or a symbolic link in the output path or a
  file to write, before removing or writing anything. `-clean` removes only regular files the
  previous `.generated` lists: `dashboards/main_test.go`, `dashboards/main_unix_test.go`.
- Every dashboard in `dashboards()` has a uid and title, is read-only, and every panel has a
  title, description and `$job` filter: `TestDashboardsAreWellFormed`.
- Each alert fires on its input and stays silent otherwise, including the README's pinned limits:
  `alerts/tests/*.yaml`. The gates reject what they exist to catch: `scripts/test-lint-alerts.sh`,
  `scripts/test-lint-dashboards.sh`.
- `sync.sh` rejects a bad manifest before fetching or writing anything: `scripts/test-sync.sh`.
- `scripts/acceptance.sh` checks the generated output's and the rules' metric names against
  `ALLOWED`. A new metric goes into `ALLOWED`, named outside any label matcher:
  `scripts/test-metric-names.sh`.

## Making a change

- A behaviour change needs a test that fails before it and passes after. Alert changes go in
  `alerts/tests/`, generator changes in `dashboards/*_test.go`, script changes in `scripts/test-*.sh`.
- An alert test lists every label and repeats the message of each alert it expects.
- Lint rules are configured in `.golangci.yml` and `.dashboard-linter.yaml`, never silenced inline.
- A runnable example is `examples/<name>/` with a name matching `[a-z0-9-]+` and an index row
  `| <task> | [name](name/) |`. Its `run.sh` starts `#!/usr/bin/env bash` then
  `source scripts/example-setup.sh`, and writes only under `$EXAMPLE_SCRATCH`, the copy
  `example_tree` returns, or a git-ignored output path. Its README's `sh` block is
  `make examples EXAMPLE=<name>`. The runner compares standard output, not standard error, with the
  README's last `text` block, ignoring spaces at line ends. Any shellcheck finding, info level
  included, fails it. Scratch copies come from `mktemp`, so set `TMPDIR` to keep them local.
  dashboard-linter prints its rule list and colour codes for every file, so a runnable example
  filters them. A runnable example needs no CHANGELOG line.
- A user-visible change adds a line under `[Unreleased]` in `CHANGELOG.md`.

## Do not

- Run a bare `go build ./...`: it tries to write a binary named `dashboards` over the directory.
- `go install` dashboard-linter: its `replace` directives make that fail. `make install-tools`
  builds it from its tag.
- Edit JSON under `dashboards/output/`: it is regenerated from the Go source.
- Treat `admin:admin` in the scripts as a secret: it is Grafana's stock login for the local stack.
