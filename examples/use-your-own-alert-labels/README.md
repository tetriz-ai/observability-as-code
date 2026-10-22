Shows `make lint-alerts` enforcing your own alert labels. `rules-payments.yaml` routes on `owner`
with `critical` and `warning` severities, so the default labels reject it. With
`ALERT_ROUTING_LABEL` and `ALERT_SEVERITIES` set, it passes lint and its two-case test, in a scratch
copy that holds only your rules.

```sh
make examples EXAMPLE=use-your-own-alert-labels
```

```text
== under the defaults (severity p0 p1 p2, routing label team)
Validating alert rules...
Checking /dev/stdin
  SUCCESS: 1 rules found

  ✓ alerts/rules-payments.yaml (promtool)
  ✗ alerts/rules-payments.yaml: alerts missing a non-empty team label (Alertmanager cannot route them): PaymentsDown
  ✗ alerts/rules-payments.yaml: severity must be one of: p0 p1 p2: PaymentsDown
== with your own labels
Validating alert rules...
Checking /dev/stdin
  SUCCESS: 1 rules found

  ✓ alerts/rules-payments.yaml (promtool)
Testing alert rules...
  SUCCESS
```
