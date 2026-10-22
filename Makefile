# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

## ─── Dashboards and alerts as code ─────────────────────────────

# Where make dashboards writes the generated JSON. Relative paths are from this directory.
DASHBOARDS_OUT ?= dashboards/output

# lint-alerts requires every alert to carry a severity from this set and a non-empty
# routing label (the label your Alertmanager routes on).
ALERT_SEVERITIES ?= p0 p1 p2
ALERT_ROUTING_LABEL ?= team

# make acceptance runs its empty Grafana as its own compose project, so `down` removes the
# container and its network without touching the dev-monitoring stack.
ACCEPTANCE_PROJECT ?= observability-as-code-acceptance

# make install-tools installs these. promtool, yq and shellcheck are Linux x86-64 downloads, so
# on macOS set TOOLS to dashboard-linter and golangci-lint.
TOOLS ?= promtool yq shellcheck dashboard-linter golangci-lint

# govulncheck runs at this version, fetched by go run.
GOVULNCHECK_VERSION ?= v1.9.0

# make examples runs one runnable example and shows its output when EXAMPLE names it, and every
# example against its README otherwise. make dashboards-sync and dashboards-upgrade act on one
# manifest entry when ONLY names it.
EXAMPLE ?=
ONLY ?=

# A make run from a recipe inherits MAKELEVEL and MAKEFLAGS and may print "Entering directory"
# lines; the examples compare their output, so they run without them.
NO_SUBMAKE = env -u MAKELEVEL -u MAKEFLAGS -u MFLAGS

.PHONY: help dashboards lint-dashboards dashboards-sync dashboards-upgrade lint-alerts test-alerts test examples check lint-go vulncheck install-tools acceptance check-dev-stack dev-monitoring dev-monitoring-down

# The generator checks the output path and removes only the files it wrote there before, which
# <dir>/.generated lists; an empty DASHBOARDS_OUT is refused here first.
dashboards: ## Generate the dashboard JSON from the Go builders and the fetched community dashboards
	@if [ -z "$(DASHBOARDS_OUT)" ]; then echo "DASHBOARDS_OUT is empty: set it to the directory the JSON goes to" >&2; exit 2; fi
	@go run ./dashboards -output "$(DASHBOARDS_OUT)" -clean

lint-dashboards: dashboards ## Generate the dashboards and lint the ones built from Go
	@command -v dashboard-linter >/dev/null 2>&1 || { echo "dashboard-linter not found: run make install-tools, or see the README's Quick start"; exit 1; }
	@fail=0; for f in "$(DASHBOARDS_OUT)"/*.json; do \
		echo "linting $$f"; \
		dashboard-linter lint --strict -c $(CURDIR)/.dashboard-linter.yaml "$$f" || fail=1; \
	done; [ "$$fail" = "0" ]

dashboards-sync: ## Fetch each community dashboard in the manifest that has no JSON yet
	@bash $(CURDIR)/dashboards/external/scripts/sync.sh $(if $(ONLY),--only "$(ONLY)")

dashboards-upgrade: ## Fetch every community dashboard in the manifest again, overwriting its JSON
	@bash $(CURDIR)/dashboards/external/scripts/sync.sh --refresh $(if $(ONLY),--only "$(ONLY)")

lint-alerts: ## Check every alert rule file with promtool and each alert's severity and routing label
	@command -v promtool >/dev/null 2>&1 || { echo "promtool not found: run make install-tools, or see the README's Quick start"; exit 1; }
	@command -v yq >/dev/null 2>&1 || { echo "yq v4 not found: run make install-tools, or see the README's Quick start"; exit 1; }
	@echo "Validating alert rules..."
	@fail=0; for f in alerts/rules*.y*ml; do \
		docs=$$(yq 'select(. != null) | document_index' "$$f" 2>/dev/null | sort -u | grep -c . ); \
		if [ "$$docs" -gt 1 ]; then echo "  ✗ $$f: holds $$docs YAML documents; promtool reads only the first, so put one PrometheusRule per file"; fail=1; continue; fi; \
		if yq 'select(. != null) | .spec' "$$f" | promtool check rules /dev/stdin; then echo "  ✓ $$f (promtool)"; else echo "  ✗ $$f: promtool check failed"; fail=1; fi; \
		missing=$$(ROUTE="$(ALERT_ROUTING_LABEL)" yq '.spec.groups[].rules[] | select(.alert != null and ((.labels[strenv(ROUTE)] // "") == "")) | .alert' "$$f"); \
		if [ -n "$$missing" ]; then \
			echo "  ✗ $$f: alerts missing a non-empty $(ALERT_ROUTING_LABEL) label (Alertmanager cannot route them): $$missing"; \
			fail=1; \
		fi; \
		rules=$$(yq ea '[select(. != null) | .spec.groups[]?.rules[]?] | length' "$$f" 2>/dev/null); \
		case "$$rules" in ''|*[!0-9]*) rules=0;; esac; \
		if [ "$$rules" -eq 0 ]; then \
			echo "  ✗ $$f: no rules reachable at .spec.groups[].rules[]. promtool reports such a file as 'SUCCESS: 0 rules found', so it would ship no alerts."; \
			fail=1; \
		fi; \
		unrouted=$$(SEV="$(ALERT_SEVERITIES)" yq '.spec.groups[].rules[] | select(.alert != null) | select(.labels.severity as $$s | (strenv(SEV) | split(" ")) | any_c(. == $$s) | not) | .alert' "$$f"); \
		if [ -n "$$unrouted" ]; then \
			echo "  ✗ $$f: severity must be one of: $(ALERT_SEVERITIES): $$unrouted"; \
			fail=1; \
		fi; \
	done; [ "$$fail" = "0" ]

test-alerts: ## Run the promtool unit tests of the alert rules
	@command -v promtool >/dev/null 2>&1 || { echo "promtool not found: run make install-tools, or see the README's Quick start"; exit 1; }
	@command -v yq >/dev/null 2>&1 || { echo "yq v4 not found: run make install-tools, or see the README's Quick start"; exit 1; }
	@echo "Testing alert rules..."
	@rm -rf alerts/.extracted && mkdir -p alerts/.extracted
	@for f in alerts/rules*.y*ml; do yq 'select(. != null) | .spec' "$$f" > "alerts/.extracted/$$(basename "$$f")"; done
	@promtool test rules alerts/tests/*.y*ml; status=$$?; rm -rf alerts/.extracted; exit $$status

# Each suite prints one ok line, or its whole output when it fails.
test: test-alerts ## Run every test that needs no Docker
	@out=$$(go test -race -count=1 ./... 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   Go tests"
	@out=$$(bash scripts/test-lint-alerts.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   alert gate tests"
	@out=$$(bash scripts/test-lint-dashboards.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   dashboard gate tests"
	@out=$$(bash scripts/test-sync.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   sync script tests"
	@out=$$(bash scripts/test-metric-names.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   metric-name extraction tests"
	@out=$$(bash scripts/test-install-ci-tools.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   CI installer tests"
	@out=$$(bash scripts/test-docker-targets.sh 2>&1) || { printf '%s\n' "$$out"; exit 1; }; echo "ok   Docker target tests"

examples: ## Run every runnable example against its README, or one with EXAMPLE=<name>
	@if [ -z "$(EXAMPLE)" ]; then $(NO_SUBMAKE) bash scripts/run-examples.sh; \
	elif [ -f "examples/$(EXAMPLE)/run.sh" ]; then $(NO_SUBMAKE) bash "examples/$(EXAMPLE)/run.sh"; \
	else echo "no runnable example named $(EXAMPLE): examples/README.md lists them" >&2; exit 2; fi

check: lint-go lint-dashboards lint-alerts test examples ## Run CI's main job: every lint, every test and every runnable example

lint-go: ## Lint the Go code with the golangci-lint version CI pins
	@v=$$(golangci-lint version --short 2>/dev/null) || { echo "golangci-lint not found: run make install-tools, or see the README's Quick start" >&2; exit 1; }; \
	want=$$(sed -n 's/^GOLANGCI_LINT_VERSION=v//p' scripts/install-ci-tools.sh); \
	[ "$$v" = "$$want" ] || { echo "golangci-lint $$v found, CI pins $$want: run make install-tools TOOLS=golangci-lint" >&2; exit 1; }
	@golangci-lint run

vulncheck: ## Check the module's dependencies and the Go standard library for known vulnerabilities
	@go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

install-tools: ## Install the pinned promtool, yq, shellcheck, dashboard-linter and golangci-lint, or only TOOLS
	@bash scripts/install-ci-tools.sh $(TOOLS)

# Startup, the checks and the teardown run in one shell: the EXIT trap removes the project whatever
# fails, a failed startup or an interrupt included, and keeps the failure's exit status.
acceptance: ## Import every dashboard into an empty Grafana in Docker and check the rules and metric names
	@down() { docker compose -p $(ACCEPTANCE_PROJECT) --profile acceptance down >/dev/null 2>&1; }; \
	trap 's=$$?; down; exit $$s' EXIT; trap 'exit 130' INT; trap 'exit 143' TERM; \
	docker compose -p $(ACCEPTANCE_PROJECT) --profile acceptance up -d --wait grafana-blank && \
		GRAFANA_URL=http://localhost:$${ACCEPTANCE_GRAFANA_PORT:-3001} bash scripts/acceptance.sh

check-dev-stack: ## Start the local Prometheus and Grafana, check they serve every dashboard, then stop them
	@bash scripts/check-dev-stack.sh

dev-monitoring: dashboards ## Generate the dashboards and start Prometheus and Grafana in Docker
	DASHBOARDS_OUT="$(abspath $(DASHBOARDS_OUT))" docker compose up -d --wait
	@echo "Monitoring ready. Dashboards at http://localhost:$${GRAFANA_PORT:-3000}, Prometheus at http://localhost:$${PROMETHEUS_PORT:-9090}"

dev-monitoring-down: ## Stop the local Prometheus and Grafana and remove their volumes
	docker compose down -v

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' Makefile | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
