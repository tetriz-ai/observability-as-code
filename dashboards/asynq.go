// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
	"github.com/grafana/grafana-foundation-sdk/go/units"
)

// Asynq Redis-backed task queue health.
//
// Source metrics come from `github.com/hibiken/asynq/x/metrics.QueueMetricsCollector`.
// Every replica that registers the collector queries the same Redis state via Inspector,
// so each (queue, state) tuple appears once per replica with identical values.
// Use `max() by (job, queue, ...)` to dedupe across replicas. `sum()` would
// multiply by replica count. Grouping by job as well keeps two services that each use a queue
// named "default" apart, so register the collector in one job per Redis: a second job exporting
// the same Redis doubles every total on this board and every asynq alert.
//
// Counters: asynq_tasks_processed_total, asynq_tasks_failed_total
// Gauges:   asynq_tasks_enqueued_total{state=active|pending|scheduled|retry|archived|completed},
//           asynq_queue_size, asynq_queue_latency_seconds,
//           asynq_queue_memory_usage_approx_bytes, asynq_queue_paused_total
//
// Rate windows use $__rate_interval — adapts automatically to the time-range
// picker. Widen via the top-right "Last 6h / 24h" selector.

const (
	asynqProcessedSel  = `asynq_tasks_processed_total{job=~"$job", queue=~"$queue"}`
	asynqFailedSel     = `asynq_tasks_failed_total{job=~"$job", queue=~"$queue"}`
	asynqProcessedRate = `rate(` + asynqProcessedSel + `[$__rate_interval])`
	asynqFailedRate    = `rate(` + asynqFailedSel + `[$__rate_interval])`
	asynqEnqueuedJob   = `asynq_tasks_enqueued_total{job=~"$job", queue=~"$queue"` // open brace, append state filter inline
	asynqQueueSize     = `asynq_queue_size{job=~"$job", queue=~"$queue"}`
	asynqLatencySecs   = `asynq_queue_latency_seconds{job=~"$job", queue=~"$queue"}`
	asynqQueueMemBytes = `asynq_queue_memory_usage_approx_bytes{job=~"$job", queue=~"$queue"}`
	asynqQueuePaused   = `asynq_queue_paused_total{job=~"$job", queue=~"$queue"}`

	// The blind-job stat counts the jobs AsynqMetricsAbsent judges: those that reported asynq
	// series in the last day and none in the last 15m, so one failed scrape does not turn it red.
	// It coalesces with `or vector(0)`: a count over no series is an empty vector, and the stat
	// would render "No data", indistinguishable from a bad datasource, where it means no job is
	// blind.
	asynqBlindJobs = `count(group by (job) (present_over_time(asynq_tasks_enqueued_total{job=~"$job"}[1d])) unless group by (job) (present_over_time(asynq_tasks_enqueued_total{job=~"$job"}[15m]))) or vector(0)`

	// Alert-condition mirror: AsynqQueueNotDraining's windowed condition minus its `for:` and its
	// paused-queue exclusion, keeping the `> 0` existence predicate — the collector emits every
	// state for every queue, so without it each idle queue draws a flat zero line. Pending must
	// stay above 0 and active stay at 0 through the window, as the rule reads them through [5m],
	// so a busy queue caught between two tasks at one instant does not show. A queue whose
	// processed counter rose in the window is draining, however short its tasks, so it does not
	// show either.
	asynqPendingNoActive = `min by (job, queue) (min_over_time(` + asynqEnqueuedJob + `, state="pending"}[$__rate_interval])) > 0 and max by (job, queue) (max_over_time(` + asynqEnqueuedJob + `, state="active"}[$__rate_interval])) == 0` +
		` unless max by (job, queue) (max_over_time(` + asynqProcessedSel + `[$__rate_interval]) - min_over_time(` + asynqProcessedSel + `[$__rate_interval])) > 0`

	// The Overview failure-rate stat shows no data under 0.1 processed tasks a second, where a few
	// retries of earlier failures would read as a red 100%.
	asynqFailureRateStat = `sum(max by (job, queue) (` + asynqFailedRate + `)) / sum(max by (job, queue) (` + asynqProcessedRate + `)) and sum(max by (job, queue) (` + asynqProcessedRate + `)) > 0.1`
)

// asynqFailShare is failed / processed over a trailing window, computed as the failure rules
// compute it: each Redis-side lifetime counter's rise inside the window (its max minus its min),
// deduped across replicas with max and min by (job, queue). Not rate(): a replica restart
// re-exports the same totals, so the rise is unaffected, whereas rate() would re-weight the young
// series. A queue first scraped inside the window gets the share of what the window holds, as in
// the rules. The series is dropped for the window when either counter reset inside it (a Redis
// key loss): a gap, not a plausible share. The processed rise must be above 0, so an idle queue
// draws no 0/0. As in the rules, a job with no processed sample in the last 5m is dropped, so a
// stopped job does not draw the frozen share its window still holds.
func asynqFailShare(window string) string {
	rise := func(sel string) string {
		return `(max by (job, queue) (max_over_time(` + sel + `[` + window + `])) - min by (job, queue) (min_over_time(` + sel + `[` + window + `])))`
	}
	reset := func(sel string) string {
		return ` unless max by (job, queue) (resets(` + sel + `[` + window + `])) > 0`
	}
	processed := rise(asynqProcessedSel)
	return rise(asynqFailedSel) + ` / ` + processed + ` and ` + processed + ` > 0` +
		reset(asynqProcessedSel) + reset(asynqFailedSel) +
		` and on (job, queue) max by (job, queue) (present_over_time(` + asynqProcessedSel + `[5m]))`
}

func asynqQueueDashboard() *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder("Asynq Queue Health").
		Uid("asynq-queue-health").
		Description("Queue health from asynq's x/metrics.QueueMetricsCollector, per job and queue. Register the collector in one job per Redis: a second job exporting the same Redis doubles every total on this board and every asynq alert, so drop that job's asynq_ series with metric_relabel_configs.").
		Tags([]string{"asynq", "queue", "redis"}).
		Readonly().
		Tooltip(dashboard.DashboardCursorSyncCrosshair).
		Refresh("30s").
		Time("now-24h", "now").
		Timezone(common.TimeZoneBrowser).
		WithVariable(datasourceVariable()).
		WithVariable(jobVariable("asynq_queue_size")).
		WithVariable(asynqQueueVariable()).

		// Overview row — always expanded
		WithRow(dashboard.NewRowBuilder("Overview")).
		WithPanel(
			countStat(
				"Pending Backlog",
				"Total tasks waiting to be picked up across all queues (state=pending). Excludes retry, scheduled, archived. max() per queue dedupes identical values reported by every pod.",
				`sum(max(`+asynqEnqueuedJob+`, state="pending"}) by (job, queue))`,
				greenYellowRedThresholds(100, 1000),
			).Span(5).Height(4),
		).
		WithPanel(
			statPanelWithThresholds(
				"Oldest Pending Age",
				"Largest queue_latency_seconds across queues — how long the oldest pending task has been waiting. Climbing values mean the worker pool cannot keep up.",
				`max(`+asynqLatencySecs+`)`,
				units.Seconds,
				greenYellowRedThresholds(60, 600),
			).Span(5).Height(4),
		).
		WithPanel(
			statPanelWithThresholds(
				"Throughput (tasks/sec)",
				"Aggregate processed-task rate across queues using $__rate_interval. processed_total counts both succeeded and failed tasks (asynq semantics). Shown in one neutral colour: a rate is neither healthy nor unhealthy on its own.",
				`sum(max(`+asynqProcessedRate+`) by (job, queue))`,
				units.Short,
				neutralThresholds(),
			).Span(5).Height(4),
		).
		WithPanel(
			statPanelWithThresholds(
				"Failure Rate %",
				"Failed-task rate divided by processed-task rate, dashboard-wide, each deduped across replicas per job and queue first, so a job with more replicas does not weigh more. No data under 0.1 processed tasks a second, where a few retries of earlier failures would read as a high share.",
				asynqFailureRateStat,
				units.PercentUnit,
				greenYellowRedThresholds(0.01, 0.05),
			).Span(5).Height(4),
		).
		WithPanel(
			countStat(
				"Jobs Blind to asynq",
				"Selected jobs that reported asynq series in the last day and none in the last 15m, the jobs AsynqMetricsAbsent pages for. The x/metrics collector drops every series when one Inspector call to Redis fails, so such a job's queue panels and asynq alerts are blind, not idle. A Prometheus that has never scraped asynq reads 0 here, and AsynqMetricsAbsent covers that case too.",
				asynqBlindJobs,
				greenRedThresholds(1),
			).Span(4).Height(4),
		).

		// Throughput row — collapsed (nested panels force collapsed=true)
		WithRow(asynqThroughputRow()).

		// Backlog row — collapsed
		WithRow(asynqBacklogRow()).

		// Alert conditions — collapsed; the raw signal behind three rules-asynq.yaml rules
		WithRow(asynqAlertConditionsRow()).

		// Resources row — collapsed
		WithRow(asynqResourcesRow())
}

// asynqAlertConditionsRow shows the raw signal behind three rules in
// alerts/rules-asynq.yaml — AsynqQueueNotDraining, AsynqQueueFailureRateHigh,
// AsynqQueueFailuresLowVolume — as each rule's expression minus its `for:` and numeric
// thresholds, so an on-call paged by one of them sees its input here. The other four rules
// read panels that already exist: AsynqQueuePaused → Resources & State
// "Paused Queues"; AsynqQueueBacklogHigh and AsynqOldestPendingStale → Backlog by State and
// Throughput (without the rules' paused-queue exclusion); AsynqMetricsAbsent → Overview
// "Jobs Blind to asynq". Thresholds and dwell times live in the rule file, not here.
func asynqAlertConditionsRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Alert Conditions (three rules-asynq.yaml signals)").
		WithPanel(
			byQueueTimeseries(
				"Pending With No Active Worker",
				"AsynqQueueNotDraining's windowed condition: the lowest pending count on queues whose pending count stayed above 0 and whose active count stayed at 0 through the window and whose processed count did not rise, work waiting that nothing is pulling. The window is the panel's rate interval here and 5m in the rule, so a busy queue caught between tasks does not show, and a rising processed count means the queue drains, however short its tasks. The rule (p1) judges this per queue after a 30m dwell, skipping paused queues (shown here regardless). One cause: the worker pool is saturated and a low-priority queue keeps losing the draw.",
				asynqPendingNoActive,
				units.Short,
			).Span(24).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Failure Share, Trailing 1h",
				"failed / processed over the last hour as exact counter differences (processed counts successes and failures; each retry attempt counts once; a dependency that rejects the call (a rate limit, for one) counts as a failure). Unlike the Throughput row's Failure Rate %, which is the same ratio over the picker's rate window and reacts faster, this is stable across a replica restart. AsynqQueueFailureRateHigh (p2) judges a sustained high share with a minimum hourly volume. Empty when nothing was processed in the hour, when a Redis counter reset inside it — a gap, not a number — or when the job has reported nothing for 5m.",
				asynqFailShare("1h"),
				units.PercentUnit,
			).Span(12).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Failure Share, Trailing 24h",
				"The same share over 24h, for queues too quiet for the hourly panel to see. AsynqQueueFailuresLowVolume (p2) judges it with a minimum failure count and a daily volume ceiling that abuts the hourly rule's floor.",
				asynqFailShare("24h"),
				units.PercentUnit,
			).Span(12).Height(8),
		)
}

func asynqThroughputRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Throughput").
		WithPanel(
			byQueueTimeseries(
				"Tasks Processed (per sec)",
				"Per-queue processed rate. processed_total counts both succeeded and failed; subtract failure-rate panel to get success rate. max(rate) dedupes per-pod duplication.",
				`max(`+asynqProcessedRate+`) by (job, queue)`,
				units.Short,
			).Span(8).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Failures (per sec)",
				"Per-queue failure rate. Spikes warrant a peek into asynq archived state and worker logs.",
				`max(`+asynqFailedRate+`) by (job, queue)`,
				units.Short,
			).Span(8).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Failure Rate %",
				"Failed / processed per queue. Ratio is duplication-safe — per-pod factor cancels.",
				`sum(`+asynqFailedRate+`) by (job, queue) / sum(`+asynqProcessedRate+`) by (job, queue)`,
				units.PercentUnit,
			).Span(8).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Oldest Pending Age",
				"How long the oldest pending task in each queue has been waiting. Climbing means worker pool is saturated. Resets to 0 when the queue drains.",
				`max(`+asynqLatencySecs+`) by (job, queue)`,
				units.Seconds,
			).Span(8).Height(8),
		)
}

func asynqBacklogRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Backlog by State").
		WithPanel(
			byQueueTimeseries(
				"Pending Tasks",
				"Tasks queued and waiting for a worker to claim them (state=pending). Steady-state should be near zero; sustained growth indicates worker shortage or a stuck handler.",
				`max(`+asynqEnqueuedJob+`, state="pending"}) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Retry Tasks",
				"Tasks that failed and are scheduled for re-execution (state=retry). High counts here usually point to a flaky downstream dependency or a deterministic bug in the handler.",
				`max(`+asynqEnqueuedJob+`, state="retry"}) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Archived (Dead) Tasks",
				"Tasks that exhausted their retries and were moved to the archived queue (state=archived). asynq keeps them until they are deleted, or for 90 days and at most 10,000 per queue.",
				`max(`+asynqEnqueuedJob+`, state="archived"}) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Active Tasks (in-flight)",
				"Tasks currently being processed by a worker (state=active). Bounded by asynq.Config.Concurrency per worker process times the queue priority share.",
				`max(`+asynqEnqueuedJob+`, state="active"}) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Scheduled Tasks",
				"Tasks queued for future execution (state=scheduled): enqueued with ProcessAt or ProcessIn and waiting for their time.",
				`max(`+asynqEnqueuedJob+`, state="scheduled"}) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Queue Size (total)",
				"Total tasks across all states per queue (active+pending+scheduled+retry+archived+completed).",
				`max(`+asynqQueueSize+`) by (job, queue)`,
				units.Short,
			).Span(6).Height(8),
		)
}

func asynqResourcesRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Resources & State").
		WithPanel(
			byQueueTimeseries(
				"Queue Memory Usage (approx)",
				"Approximate Redis memory consumed per queue, sampled by asynq. Useful for catching unbounded retry or completed_task_ttl misconfiguration.",
				`max(`+asynqQueueMemBytes+`) by (job, queue)`,
				units.BytesIEC,
			).Span(12).Height(8),
		).
		WithPanel(
			byQueueTimeseries(
				"Paused Queues",
				"1 = paused, 0 = active. Paused queues do not dispatch tasks. asynq cli can pause/unpause for incident response.",
				`max(`+asynqQueuePaused+`) by (job, queue)`,
				units.Short,
			).Span(12).Height(8),
		)
}

// byQueueTimeseries is defaultTimeseries with a caller-supplied unit and a table legend with
// one row per queue.
func byQueueTimeseries(title, description, expr, unit string) *timeseries.PanelBuilder {
	return defaultTimeseries().
		Title(title).
		Description(description).
		Unit(unit).
		Legend(tableLegend()).
		WithTarget(promQuery(expr, "{{queue}} ({{job}})"))
}

// asynqQueueVariable is a multi-select dropdown of every queue label seen in
// asynq_queue_size. "All" resolves to ".+" so the regex matcher
// queue=~"$queue" stays well-formed when no queue is filtered. Reloads on
// time-range change to surface queues that only existed in older windows.
func asynqQueueVariable() cog.Builder[dashboard.VariableModel] {
	return dashboard.NewQueryVariableBuilder("queue").
		Label("Queue").
		Datasource(datasourceRef()).
		Query(dashboard.StringOrMap{String: cog.ToPtr(`label_values(asynq_queue_size, queue)`)}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		IncludeAll(true).
		Multi(true).
		AllValue(".+").
		Current(allSelected())
}
