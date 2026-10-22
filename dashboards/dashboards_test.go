// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// built is one dashboard as Grafana receives it: the generator's JSON, decoded generically.
func built(t *testing.T, b *dashboard.DashboardBuilder) map[string]any {
	t.Helper()
	d, err := buildDashboard(b)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return m
}

// panels returns every panel, rows' nested panels included, rows excluded.
func panels(d map[string]any) []map[string]any {
	var out []map[string]any
	var walk func(list []any)
	walk = func(list []any) {
		for _, p := range list {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if pm["type"] == "row" {
				if nested, ok := pm["panels"].([]any); ok {
					walk(nested)
				}
				continue
			}
			out = append(out, pm)
		}
	}
	if list, ok := d["panels"].([]any); ok {
		walk(list)
	}
	return out
}

// panelsAndRows returns every panel and every row, rows' nested panels included.
func panelsAndRows(d map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := d["panels"].([]any)
	for _, p := range list {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, pm)
		nested, _ := pm["panels"].([]any)
		for _, n := range nested {
			if nm, ok := n.(map[string]any); ok {
				out = append(out, nm)
			}
		}
	}
	return out
}

func panelByTitle(t *testing.T, d map[string]any, title string) map[string]any {
	t.Helper()
	for _, p := range panels(d) {
		if p["title"] == title {
			return p
		}
	}
	t.Fatalf("no panel titled %q", title)
	return nil
}

func exprs(p map[string]any) []string {
	var out []string
	targets, _ := p["targets"].([]any)
	for _, tg := range targets {
		if tm, ok := tg.(map[string]any); ok {
			if e, ok := tm["expr"].(string); ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// The generator writes exactly these dashboards; a builder dropped from the list, or the list
// left empty, fails here rather than passing every per-dashboard check vacuously.
func TestDashboardsList(t *testing.T) {
	var got []string
	for _, d := range dashboards() {
		got = append(got, d.name)
	}
	want := []string{"asynq-queue-health", "redis-command-health"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dashboards() = %v, want %v", got, want)
	}
}

func TestDashboardsAreWellFormed(t *testing.T) {
	uids := map[string]string{}
	for _, def := range dashboards() {
		name := def.name
		d := built(t, def.builder)
		uid, _ := d["uid"].(string)
		title, _ := d["title"].(string)
		if uid == "" || title == "" {
			t.Errorf("%s: uid %q, title %q: both must be set", name, uid, title)
		}
		if other, dup := uids[uid]; dup {
			t.Errorf("%s and %s share uid %q: one would overwrite the other on import", name, other, uid)
		}
		uids[uid] = name
		if editable, _ := d["editable"].(bool); editable {
			t.Errorf("%s: editable; generated dashboards are read-only so edits go through the Go source", name)
		}
		ps := panels(d)
		if len(ps) == 0 {
			t.Errorf("%s: no panels", name)
		}
		// Grafana's query API keys each result by refId, and a link to one panel names its id.
		ids := map[float64]any{}
		for _, p := range panelsAndRows(d) {
			if id, ok := p["id"].(float64); !ok || id <= 0 {
				t.Errorf("%s: %s %q has id %v, want a positive number", name, p["type"], p["title"], p["id"])
			} else if other, dup := ids[id]; dup {
				t.Errorf("%s: %q and %q share id %v", name, other, p["title"], id)
			} else {
				ids[id] = p["title"]
			}
			refs := map[string]bool{}
			targets, _ := p["targets"].([]any)
			for _, tg := range targets {
				tm, _ := tg.(map[string]any)
				ref, _ := tm["refId"].(string)
				if ref == "" || refs[ref] {
					t.Errorf("%s: panel %q has a query with refId %q, want one unique within the panel", name, p["title"], ref)
				}
				refs[ref] = true
			}
		}
		for _, p := range ps {
			if p["title"] == "" || p["description"] == "" || p["description"] == nil {
				t.Errorf("%s: panel %q needs a title and a description", name, p["title"])
			}
			es := exprs(p)
			if len(es) == 0 {
				t.Errorf("%s: panel %q has no query", name, p["title"])
			}
			for _, e := range es {
				if !strings.Contains(e, `job=~"$job"`) {
					t.Errorf("%s: panel %q query ignores $job: %s", name, p["title"], e)
				}
			}
		}
	}
}

// A rate is neither healthy nor unhealthy on its own, so the throughput stat has one neutral
// step: no reading turns it red or green.
func TestAsynqThroughputStatIsNeutral(t *testing.T) {
	p := panelByTitle(t, built(t, asynqQueueDashboard()), "Throughput (tasks/sec)")
	fc, _ := p["fieldConfig"].(map[string]any)
	defaults, _ := fc["defaults"].(map[string]any)
	th, _ := defaults["thresholds"].(map[string]any)
	got := th["steps"]
	want := []any{map[string]any{"value": nil, "color": "blue"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("throughput thresholds = %v, want one neutral step %v", got, want)
	}
}

// The go-redis hook labels each client `instance`; Prometheus stores it as exported_instance
// under the default honor_labels: false. Every query must filter and group on that label, or
// every panel is "No data".
func TestRedisQueriesUseTheHooksClientLabel(t *testing.T) {
	d := built(t, redisCommandDashboard())
	for _, p := range panels(d) {
		for _, e := range exprs(p) {
			if !strings.Contains(e, `exported_instance=~"$client"`) {
				t.Errorf("panel %q does not filter on exported_instance: %s", p["title"], e)
			}
		}
	}
	templating, _ := d["templating"].(map[string]any)
	list, _ := templating["list"].([]any)
	found := false
	for _, v := range list {
		vm, _ := v.(map[string]any)
		if vm["name"] == "client" {
			found = true
			if q, _ := vm["query"].(string); q != "label_values(redis_single_commands_count, exported_instance)" {
				t.Errorf("client variable query = %q", q)
			}
		}
	}
	if !found {
		t.Error("no client variable")
	}
}

// promtoolTest runs `promtool test rules` on one rule group and one test file. Without
// promtool the dashboard PromQL goes untested, so a missing promtool fails the test unless
// OAC_SKIP_PROMTOOL=1 says the skip is deliberate.
func promtoolTest(t *testing.T, rules, tests string) {
	t.Helper()
	bin, err := exec.LookPath("promtool")
	if err != nil {
		if os.Getenv("OAC_SKIP_PROMTOOL") == "1" {
			t.Skip("promtool not on PATH and OAC_SKIP_PROMTOOL=1")
		}
		t.Fatal("promtool not on PATH: install it from https://prometheus.io/download/ (it ships with Prometheus), or set OAC_SKIP_PROMTOOL=1 to skip the PromQL tests")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(tests), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "test", "rules", "test.yaml")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		t.Fatalf("promtool test rules failed:\n%s", out)
	}
	if err != nil {
		t.Fatalf("running promtool: %v", err)
	}
}

// The failure-share panels compute the share as the failure rules do, and drop the series (a
// gap) when either Redis counter went backwards inside the window, instead of drawing a
// plausible share or +Inf.
func TestAsynqFailShareGapsOnBackwardsCounters(t *testing.T) {
	expr := strings.NewReplacer(`$job`, `.+`, `$queue`, `.+`).Replace(asynqFailShare("1h"))
	exprJSON, _ := json.Marshal(expr)
	rules := "groups:\n  - name: g\n    rules:\n      - record: fail_share\n        expr: " + string(exprJSON) + "\n"
	tests := `rule_files: [rules.yaml]
evaluation_interval: 1m
tests:
  # failed drops 100 -> 10 (a key loss) at 61m while processed keeps rising: a gap while the
  # drop is inside the window.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '100x60 10x61'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+10x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 110m
        exp_samples: []
  # processed drops (a key loss) at 61m while failed is flat: a gap.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '5x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '1000x60 100x61'
    promql_expr_test:
      - expr: fail_share
        eval_time: 110m
        exp_samples: []
  # processed resets at 61m and climbs past its old value; failed keeps rising: a gap.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+10x60 0+30x60'
    promql_expr_test:
      - expr: fail_share
        eval_time: 110m
        exp_samples: []
  # failed resets at 60m and climbs past its old value; processed keeps rising: a gap, as in
  # the rules, not the plausible share the differences would give.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x60 0+3x59'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+4x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 110m
        exp_samples: []
  # 1 failure per 4 processed for two hours: 25% over the last hour.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+4x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 120m
        exp_samples:
          - labels: 'fail_share{job="worker", queue="q"}'
            value: 0.25
  # failed resets between the samples at 60m and 61m, the window's open left edge, then climbs
  # 3 a minute: the window holds only the climb, 177 failed of 236 processed, as the rule
  # reads it.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x60 0+3x70'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+4x130'
    promql_expr_test:
      - expr: fail_share
        eval_time: 120m
        exp_samples:
          - labels: 'fail_share{job="worker", queue="q"}'
            value: 0.75
  # The same input at 119m: the reset is now between the window's first two samples, at 60m
  # and 61m, so it is inside the window: a gap.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x60 0+3x70'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+4x130'
    promql_expr_test:
      - expr: fail_share
        eval_time: 119m
        exp_samples: []
  # Both counters reset at 71m (the Redis keys lost together) and climb again: a gap.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+1x70 0+1x49'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+4x70 0+4x49'
    promql_expr_test:
      - expr: fail_share
        eval_time: 110m
        exp_samples: []
  # An idle queue, both counters flat at 5 failed and 50 processed: no series, not 0/0.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '5x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '50x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 120m
        exp_samples: []
  # A queue first scraped at 30m, half its tasks failing: a share at 75m from the 45 minutes the
  # window holds, as the rule reads it.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '_x30 0+1x90'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '_x30 0+2x90'
    promql_expr_test:
      - expr: fail_share
        eval_time: 75m
        exp_samples:
          - labels: 'fail_share{job="worker", queue="q"}'
            value: 0.5
  # Three replicas export the same totals: one series for the queue, not three.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q", instance="pod-a"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q", instance="pod-a"}'
        values: '0+4x120'
      - series: 'asynq_tasks_failed_total{job="worker", queue="q", instance="pod-b"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q", instance="pod-b"}'
        values: '0+4x120'
      - series: 'asynq_tasks_failed_total{job="worker", queue="q", instance="pod-c"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q", instance="pod-c"}'
        values: '0+4x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 120m
        exp_samples:
          - labels: 'fail_share{job="worker", queue="q"}'
            value: 0.25
  # The job stops reporting at 10m, half its tasks failed: no series at 45m, not the frozen 50%
  # the hour still holds, as the rules drop a job with no processed sample in 5m.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="worker", queue="q"}'
        values: '0+5x10'
      - series: 'asynq_tasks_processed_total{job="worker", queue="q"}'
        values: '0+10x10'
    promql_expr_test:
      - expr: fail_share
        eval_time: 45m
        exp_samples: []
  # Two jobs each with a queue named default: kept apart, 25% and 50%.
  - interval: 1m
    input_series:
      - series: 'asynq_tasks_failed_total{job="billing", queue="default"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="billing", queue="default"}'
        values: '0+4x120'
      - series: 'asynq_tasks_failed_total{job="mailer", queue="default"}'
        values: '0+1x120'
      - series: 'asynq_tasks_processed_total{job="mailer", queue="default"}'
        values: '0+2x120'
    promql_expr_test:
      - expr: fail_share
        eval_time: 120m
        exp_samples:
          - labels: 'fail_share{job="billing", queue="default"}'
            value: 0.25
          - labels: 'fail_share{job="mailer", queue="default"}'
            value: 0.5
`
	promtoolTest(t, rules, tests)
}

// The failure-share panel reads the share AsynqQueueFailureRateHigh judges. On a replica restart
// (pod-a exports the totals until 60m, pod-b the same totals from 61m) with failures stopping at
// 45m, both give 70 failed of 590 processed at 90m.
func TestAsynqFailSharePanelMatchesTheRule(t *testing.T) {
	panel := strings.NewReplacer(`$job`, `.+`, `$queue`, `.+`).Replace(asynqFailShare("1h"))
	rule, _, ok := strings.Cut(ruleExpr(t, "rules-asynq.yaml", "AsynqQueueFailureRateHigh"), " > 0.25")
	if !ok {
		t.Fatal(`AsynqQueueFailureRateHigh has no " > 0.25" to cut its ratio at`)
	}
	rule = unwrap(rule)
	panelJSON, _ := json.Marshal(panel)
	ruleJSON, _ := json.Marshal(rule)
	rules := "groups:\n  - name: g\n    rules:\n" +
		"      - record: fail_share\n        expr: " + string(panelJSON) + "\n" +
		"      - record: rule_share\n        expr: " + string(ruleJSON) + "\n"
	in := series(`asynq_tasks_failed_total{job="worker", queue="q", instance="pod-a"}`, "0+5x45 225x15") +
		series(`asynq_tasks_processed_total{job="worker", queue="q", instance="pod-a"}`, "0+10x60") +
		series(`asynq_tasks_failed_total{job="worker", queue="q", instance="pod-b"}`, "_x61 225x29") +
		series(`asynq_tasks_processed_total{job="worker", queue="q", instance="pod-b"}`, "_x61 610+10x29")
	tests := `rule_files: [rules.yaml]
evaluation_interval: 1m
tests:
  - interval: 1m
    input_series:
` + in + `    promql_expr_test:
      - expr: fail_share
        eval_time: 90m
        exp_samples:
          - labels: 'fail_share{job="worker", queue="q"}'
            value: 0.11864406779661017
      - expr: rule_share
        eval_time: 90m
        exp_samples:
          - labels: 'rule_share{job="worker", queue="q"}'
            value: 0.11864406779661017
`
	promtoolTest(t, rules, tests)
}

// ruleExpr returns the expression of alert in alerts/<file>, read from its `expr: |` block with
// each line trimmed and joined by a space.
func ruleExpr(t *testing.T, file, alert string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "alerts", file))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "- alert: "+alert+"\n")
	if !ok {
		t.Fatalf("no alert %s in %s", alert, file)
	}
	_, rest, ok = strings.Cut(rest, "expr: |\n")
	if !ok {
		t.Fatalf("alert %s in %s has no expr: | block", alert, file)
	}
	var lines []string
	for _, l := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(l, "            ") {
			break
		}
		lines = append(lines, strings.TrimSpace(l))
	}
	return strings.Join(lines, " ")
}

// unwrap drops the unmatched opening parentheses that start s, which cutting an expression
// inside the parentheses around it leaves behind.
func unwrap(s string) string {
	for strings.HasPrefix(s, "(") && strings.Count(s, "(") > strings.Count(s, ")") {
		s = strings.TrimSpace(s[1:])
	}
	return s
}

// panelExpr returns the first query of the panel of type typ titled title, as Prometheus would
// receive it with every variable at All and $__rate_interval at 5m.
func panelExpr(t *testing.T, d map[string]any, title, typ string) string {
	t.Helper()
	for _, p := range panels(d) {
		if p["title"] == title && p["type"] == typ {
			return strings.NewReplacer(`$job`, `.+`, `$queue`, `.+`, `$client`, `.+`, `$__rate_interval`, `5m`).Replace(exprs(p)[0])
		}
	}
	t.Fatalf("no %s panel titled %q", typ, title)
	return ""
}

// exprTest evaluates expr at evalTime over in, a promtool input_series block sampled every
// minute, and expects samples, a promtool exp_samples list such as "[]".
func exprTest(t *testing.T, expr, in, evalTime, samples string) {
	t.Helper()
	exprTestEvery(t, "1m", expr, in, evalTime, samples)
}

// exprTestEvery is exprTest with in sampled every interval.
func exprTestEvery(t *testing.T, interval, expr, in, evalTime, samples string) {
	t.Helper()
	exprJSON, _ := json.Marshal(expr)
	tests := `rule_files: [rules.yaml]
evaluation_interval: 1m
tests:
  - interval: ` + interval + `
    input_series:
` + in + `    promql_expr_test:
      - expr: ` + string(exprJSON) + `
        eval_time: ` + evalTime + `
        exp_samples: ` + samples + `
`
	promtoolTest(t, "groups: []\n", tests)
}

// sample is a promtool exp_samples list holding one sample.
func sample(labels string, value float64) string {
	return "[{labels: '" + labels + "', value: " + strconv.FormatFloat(value, 'g', -1, 64) + "}]"
}

// failureRateStat evaluates the Overview "Failure Rate %" stat at 15m over in, a promtool
// input_series block, and expects samples.
func failureRateStat(t *testing.T, in, samples string) {
	t.Helper()
	exprTest(t, panelExpr(t, built(t, asynqQueueDashboard()), "Failure Rate %", "stat"), in, "15m", samples)
}

// series is one promtool input series line pair.
func series(selector, values string) string {
	return "      - series: '" + selector + "'\n        values: '" + values + "'\n"
}

// The Overview failure-rate stat dedupes replicas within each job and queue before summing, so a
// job with three replicas does not weigh three times. Job a: 3 replicas, 60 tasks a minute, all
// failing. Job b: 1 replica, 180 a minute, none failing. The share is 60 of 240.
func TestAsynqFailureRateStatDedupesReplicas(t *testing.T) {
	in := ""
	for _, pod := range []string{"pod-a", "pod-b", "pod-c"} {
		in += series(`asynq_tasks_failed_total{job="a", queue="q", instance="`+pod+`"}`, "0+60x20")
		in += series(`asynq_tasks_processed_total{job="a", queue="q", instance="`+pod+`"}`, "0+60x20")
	}
	in += series(`asynq_tasks_failed_total{job="b", queue="q", instance="pod-d"}`, "0x20")
	in += series(`asynq_tasks_processed_total{job="b", queue="q", instance="pod-d"}`, "0+180x20")
	failureRateStat(t, in, sample("{}", 0.25))
}

// Two queues in one job are summed, not deduped: q1 runs 60 tasks a minute, all failing, q2 180
// a minute, none failing. The share is 60 of 240.
func TestAsynqFailureRateStatKeepsQueuesApart(t *testing.T) {
	in := series(`asynq_tasks_failed_total{job="a", queue="q1"}`, "0+60x20") +
		series(`asynq_tasks_processed_total{job="a", queue="q1"}`, "0+60x20") +
		series(`asynq_tasks_failed_total{job="a", queue="q2"}`, "0x20") +
		series(`asynq_tasks_processed_total{job="a", queue="q2"}`, "0+180x20")
	failureRateStat(t, in, sample("{}", 0.25))
}

// Under 0.1 processed tasks a second the failure-rate stat shows no data: 0.04 tasks a second,
// every one a failing retry, would otherwise read 100%. At 1 task a second, half failing, it
// reads 50%.
func TestAsynqFailureRateStatHasAVolumeFloor(t *testing.T) {
	idle := series(`asynq_tasks_failed_total{job="a", queue="q"}`, "0+2.4x20") +
		series(`asynq_tasks_processed_total{job="a", queue="q"}`, "0+2.4x20")
	t.Run("0.04 a second", func(t *testing.T) { failureRateStat(t, idle, "[]") })
	busy := series(`asynq_tasks_failed_total{job="a", queue="q"}`, "0+30x20") +
		series(`asynq_tasks_processed_total{job="a", queue="q"}`, "0+60x20")
	t.Run("1 a second", func(t *testing.T) { failureRateStat(t, busy, sample("{}", 0.5)) })
	// Either side of the floor, sampled every 100s so the rates come out exact: 0.09 tasks a
	// second shows no data, 0.11 a second, half failing, reads 50%.
	under := "      - series: 'asynq_tasks_failed_total{job=\"a\", queue=\"q\"}'\n        values: '0+4.5x20'\n" +
		"      - series: 'asynq_tasks_processed_total{job=\"a\", queue=\"q\"}'\n        values: '0+9x20'\n"
	over := "      - series: 'asynq_tasks_failed_total{job=\"a\", queue=\"q\"}'\n        values: '0+5.5x20'\n" +
		"      - series: 'asynq_tasks_processed_total{job=\"a\", queue=\"q\"}'\n        values: '0+11x20'\n"
	expr := panelExpr(t, built(t, asynqQueueDashboard()), "Failure Rate %", "stat")
	t.Run("0.09 a second", func(t *testing.T) { exprTestEvery(t, "100s", expr, under, "15m", "[]") })
	t.Run("0.11 a second", func(t *testing.T) { exprTestEvery(t, "100s", expr, over, "15m", sample("{}", 0.5)) })
}

// The Redis error-rate stat shows no data when no command ran, not a 0/0 NaN on green. At 100
// commands a second with 1 failing it reads 1%.
func TestRedisErrorRateStatIsEmptyWithoutTraffic(t *testing.T) {
	expr := panelExpr(t, built(t, redisCommandDashboard()), "Command Error Rate %", "stat")
	idle := series(`redis_single_commands_count{job="api", exported_instance="cache", command="get"}`, "500x20") +
		series(`redis_single_errors{job="api", exported_instance="cache", command="get"}`, "3x20")
	t.Run("no traffic", func(t *testing.T) { exprTest(t, expr, idle, "15m", "[]") })
	busy := series(`redis_single_commands_count{job="api", exported_instance="cache", command="get"}`, "0+6000x20") +
		series(`redis_single_errors{job="api", exported_instance="cache", command="get"}`, "0+60x20")
	t.Run("100 a second", func(t *testing.T) { exprTest(t, expr, busy, "15m", sample("{}", 0.01)) })
	// A client with traffic and no error series yet (the hook creates it on the first error)
	// reads 0, not no data.
	clean := series(`redis_single_commands_count{job="api", exported_instance="cache", command="get"}`, "0+6000x20")
	t.Run("no error series", func(t *testing.T) { exprTest(t, expr, clean, "15m", sample("{}", 0)) })
	// Half a command a second, 1% failing, sampled every 100s so the rates come out exact.
	slow := "      - series: 'redis_single_commands_count{job=\"api\", exported_instance=\"cache\", command=\"get\"}'\n        values: '0+50x20'\n" +
		"      - series: 'redis_single_errors{job=\"api\", exported_instance=\"cache\", command=\"get\"}'\n        values: '0+0.5x20'\n"
	t.Run("0.5 a second", func(t *testing.T) { exprTestEvery(t, "100s", expr, slow, "15m", sample("{}", 0.01)) })
}

// The blind-job stat counts the jobs AsynqMetricsAbsent judges: worker went blind at 30m (a Redis
// outage drops every series its collector exports) while svc-b still exports its six, so it
// reads 1 at 60m, and 0 at 20m, before the outage.
func TestAsynqBlindJobsStat(t *testing.T) {
	expr := panelExpr(t, built(t, asynqQueueDashboard()), "Jobs Blind to asynq", "stat")
	in := series(`asynq_tasks_enqueued_total{job="worker", queue="default", state="pending"}`, "0x30")
	for _, q := range []string{"q1", "q2"} {
		for _, state := range []string{"pending", "active", "retry"} {
			in += series(`asynq_tasks_enqueued_total{job="svc-b", queue="`+q+`", state="`+state+`"}`, "0x60")
		}
	}
	t.Run("worker blind", func(t *testing.T) { exprTest(t, expr, in, "60m", sample("{}", 1)) })
	t.Run("before the outage", func(t *testing.T) { exprTest(t, expr, in, "20m", sample("{}", 0)) })
	// worker alone, silent since 30m: still counted at 150m, its last series inside the day.
	worker := series(`asynq_tasks_enqueued_total{job="worker", queue="default", state="pending"}`, "0x30")
	t.Run("blind for 2h", func(t *testing.T) { exprTest(t, expr, worker, "150m", sample("{}", 1)) })
	// One failed scrape (a staleness marker at 10m) is not a blind job: the stat, like
	// AsynqMetricsAbsent, waits for 15m without a sample.
	scrape := series(`asynq_tasks_enqueued_total{job="worker", queue="default", state="pending"}`, "0x9 stale 0x10")
	t.Run("one failed scrape", func(t *testing.T) { exprTest(t, expr, scrape, "10m", sample("{}", 0)) })
}

// The no-active-worker panel is the rule's windowed condition: a busy queue that shows pending 1
// and active 0 at one instant, with workers active on it moments before, does not appear; a queue
// stuck at 5 pending with nothing active does.
func TestAsynqPendingNoActivePanelIsWindowed(t *testing.T) {
	expr := panelExpr(t, built(t, asynqQueueDashboard()), "Pending With No Active Worker", "timeseries")
	in := series(`asynq_tasks_enqueued_total{job="w", queue="busy", state="pending"}`, "0x9 1 0x10") +
		series(`asynq_tasks_enqueued_total{job="w", queue="busy", state="active"}`, "1x9 0 1x10") +
		series(`asynq_tasks_enqueued_total{job="w", queue="stuck", state="pending"}`, "5x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="stuck", state="active"}`, "0x20")
	exprTest(t, expr, in, "10m", sample(`{job="w", queue="stuck"}`, 5))
	// Each window on its own: 5 pending throughout with a worker active until a minute ago, and
	// nothing active throughout with a task pending only at this instant. Neither shows.
	active := series(`asynq_tasks_enqueued_total{job="w", queue="q", state="pending"}`, "5x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="q", state="active"}`, "1x9 0 1x10")
	t.Run("active window", func(t *testing.T) { exprTest(t, expr, active, "10m", "[]") })
	pending := series(`asynq_tasks_enqueued_total{job="w", queue="q", state="pending"}`, "0x9 1 0x10") +
		series(`asynq_tasks_enqueued_total{job="w", queue="q", state="active"}`, "0x20")
	t.Run("pending window", func(t *testing.T) { exprTest(t, expr, pending, "10m", "[]") })
}

// The no-active-worker panel reads AsynqQueueNotDraining's condition (its expression without the
// paused-queue exclusion) with the 5m window: on a stuck queue, a busy one, an idle one and a
// fast one whose processed counter rises while pending never reaches 0, both give the same
// samples at 10m.
func TestAsynqPendingNoActivePanelMatchesTheRule(t *testing.T) {
	panel := panelExpr(t, built(t, asynqQueueDashboard()), "Pending With No Active Worker", "timeseries")
	const paused = " unless max by (job, queue) (max_over_time(asynq_queue_paused_total[5m])) > 0"
	rule := ruleExpr(t, "rules-asynq.yaml", "AsynqQueueNotDraining")
	if !strings.Contains(rule, paused) {
		t.Fatalf("AsynqQueueNotDraining has no paused-queue exclusion %q to leave out", paused)
	}
	rule = strings.Replace(rule, paused, "", 1)
	in := series(`asynq_tasks_enqueued_total{job="w", queue="stuck", state="pending"}`, "5x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="stuck", state="active"}`, "0x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="busy", state="pending"}`, "0x9 1 0x10") +
		series(`asynq_tasks_enqueued_total{job="w", queue="busy", state="active"}`, "1x9 0 1x10") +
		series(`asynq_tasks_enqueued_total{job="w", queue="idle", state="pending"}`, "0x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="idle", state="active"}`, "0x20") +
		series(`asynq_tasks_enqueued_total{job="w", queue="fast", state="pending"}`, "1 2 1 3 1 2 1x14") +
		series(`asynq_tasks_enqueued_total{job="w", queue="fast", state="active"}`, "0x20") +
		series(`asynq_tasks_processed_total{job="w", queue="fast"}`, "0+120x20")
	for name, expr := range map[string]string{"panel": panel, "rule": rule} {
		t.Run(name, func(t *testing.T) { exprTest(t, expr, in, "10m", sample(`{job="w", queue="stuck"}`, 5)) })
	}
}

// Each panel names its queries from A again: the overall latency panel's three are A, B and C.
func TestRefIDsRestartPerPanel(t *testing.T) {
	p := panelByTitle(t, built(t, redisCommandDashboard()), "Command Latency (p50 / p95 / p99) — overall")
	var got []string
	targets, _ := p["targets"].([]any)
	for _, tg := range targets {
		tm, _ := tg.(map[string]any)
		ref, _ := tm["refId"].(string)
		got = append(got, ref)
	}
	if want := []string{"A", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("refIds %v, want %v", got, want)
	}
}

// A count stat shows a whole number: 2706 pending tasks read 2706, not 2706.00 or, under the
// short unit's scaling, 2.71 K.
func TestCountStatsHaveNoDecimals(t *testing.T) {
	d := built(t, asynqQueueDashboard())
	for _, title := range []string{"Pending Backlog", "Jobs Blind to asynq"} {
		p := panelByTitle(t, d, title)
		fc, _ := p["fieldConfig"].(map[string]any)
		defaults, _ := fc["defaults"].(map[string]any)
		if dec, ok := defaults["decimals"].(float64); !ok || dec != 0 {
			t.Errorf("%s: decimals %v, want 0", title, defaults["decimals"])
		}
		if unit := defaults["unit"]; unit != "none" {
			t.Errorf("%s: unit %v, want none, which does not scale by 1,000", title, unit)
		}
	}
}

// Each dashboard's Job list holds only the jobs that export its metrics, not every scrape job.
func TestJobVariableListsJobsWithTheDashboardsMetric(t *testing.T) {
	for _, c := range []struct {
		b    *dashboard.DashboardBuilder
		want string
	}{
		{asynqQueueDashboard(), "label_values(asynq_queue_size, job)"},
		{redisCommandDashboard(), "label_values(redis_single_commands_count, job)"},
	} {
		d := built(t, c.b)
		templating, _ := d["templating"].(map[string]any)
		list, _ := templating["list"].([]any)
		var got any
		for _, v := range list {
			if vm, _ := v.(map[string]any); vm["name"] == "job" {
				got = vm["query"]
			}
		}
		if got != c.want {
			t.Errorf("%s: job variable query %v, want %q", d["title"], got, c.want)
		}
	}
}

// GRPCHighLatency's share through a rollout: pod a, every call fast, is replaced at 20m by pod b,
// every call fast. At 25m the share is 0, not below 0, and GRPCLatencyBucketMissing counts no
// call as bucketless, because a's bucket is read over the same 10m window as its rates.
func TestGRPCHighLatencyShareThroughARollout(t *testing.T) {
	share, _, ok := strings.Cut(ruleExpr(t, "rules-platform.yaml", "GRPCHighLatency"), " > 0.010000001")
	if !ok {
		t.Fatal(`GRPCHighLatency has no " > 0.010000001" to cut its share at`)
	}
	bucketless, _, ok := strings.Cut(ruleExpr(t, "rules-platform.yaml", "GRPCLatencyBucketMissing"), " > 0.05")
	if !ok {
		t.Fatal(`GRPCLatencyBucketMissing has no " > 0.05" to cut its rate at`)
	}
	in := series(`grpc_server_handling_seconds_bucket{job="api", instance="a", grpc_service="s", grpc_method="m", le="2.5"}`, "0+60x20") +
		series(`grpc_server_handling_seconds_count{job="api", instance="a", grpc_service="s", grpc_method="m"}`, "0+60x20") +
		series(`grpc_server_handling_seconds_bucket{job="api", instance="b", grpc_service="s", grpc_method="m", le="2.5"}`, "_x20 0+60x40") +
		series(`grpc_server_handling_seconds_count{job="api", instance="b", grpc_service="s", grpc_method="m"}`, "_x20 0+60x40")
	t.Run("GRPCHighLatency share", func(t *testing.T) {
		exprTest(t, unwrap(share), in, "25m", sample(`{grpc_service="s", grpc_method="m"}`, 0))
	})
	t.Run("GRPCLatencyBucketMissing rate", func(t *testing.T) { exprTest(t, unwrap(bucketless), in, "25m", "[]") })
}
