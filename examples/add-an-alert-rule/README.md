Adds a rule of your own with its unit test: an error-rate alert for a gRPC checkout service, in
`rules-checkout.yaml`, and three cases in `checkout_alerts_test.yaml` that assert it fires at 10%
errors and stays silent at 0.8% and under its traffic floor. The script copies both files into a
scratch copy of this repository, where you would put them in `alerts/` and `alerts/tests/`, then
runs the same gates CI runs. The new file appears in the lint output and as a third `SUCCESS`.

```sh
make examples EXAMPLE=add-an-alert-rule
```

```text
Validating alert rules...
Checking /dev/stdin
  SUCCESS: 7 rules found

  ✓ alerts/rules-asynq.yaml (promtool)
Checking /dev/stdin
  SUCCESS: 1 rules found

  ✓ alerts/rules-checkout.yaml (promtool)
Checking /dev/stdin
  SUCCESS: 5 rules found

  ✓ alerts/rules-platform.yaml (promtool)
Testing alert rules...
  SUCCESS

  SUCCESS

  SUCCESS
```
