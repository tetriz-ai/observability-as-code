# Alert Rules

The rule files are PrometheusRule resources, the prometheus-operator's custom resource. The rules
hold no cluster-specific values, so one file serves every cluster. A Prometheus without the
operator loads each file's `spec`.

Validate them with `make lint-alerts` and unit-test them with `make test-alerts`. The tests in
`tests/` assert that each alert fires on the input it is meant for and stays silent otherwise.

`metadata.labels` must match your Prometheus `ruleSelector`. `role: alert-rules` is a placeholder.
