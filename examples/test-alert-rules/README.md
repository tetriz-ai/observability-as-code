Runs the two alert gates CI runs on every pull request. `make lint-alerts` checks each rule file
with `promtool check rules` and checks every alert's severity and routing label.
`make test-alerts` runs the promtool unit tests in `alerts/tests/`, which assert that each alert
fires on the input it is meant for and stays silent otherwise.

```sh
make examples EXAMPLE=test-alert-rules
```

```text
Validating alert rules...
Checking /dev/stdin
  SUCCESS: 7 rules found

  ✓ alerts/rules-asynq.yaml (promtool)
Checking /dev/stdin
  SUCCESS: 5 rules found

  ✓ alerts/rules-platform.yaml (promtool)
Testing alert rules...
  SUCCESS

  SUCCESS
```
