Shows the alert tests catching a mistake. The script takes the checkout rule and its test from
[add-an-alert-rule](../add-an-alert-rule/), types the 5% threshold as `0.5`, and runs
`make test-alerts` on a scratch copy of the repository. The case that should fire at 10% errors
gets no alert, so the target fails, and the script exits 0 only because it did.

```sh
make examples EXAMPLE=catch-a-broken-alert
```

```text
Testing alert rules...
  SUCCESS

  FAILED:
    alertname: CheckoutErrorRateHigh, time: 20m,
        exp:[
            0:
              Labels:{alertname="CheckoutErrorRateHigh", severity="p1", team="checkout"}
              Annotations:{description="10% of checkout calls failed over the last 5m.", summary="Checkout error rate above 5%"}
            ],
        got:[]


  SUCCESS

make test-alerts failed: the case that should fire at 10% errors got no alert
```
