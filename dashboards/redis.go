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

// Redis command health, per Redis client.
//
// Source metrics are the go-redis hook metric names of
// github.com/globocom/go-redis-prometheus (labels: the client name, command):
//   redis_single_commands {_bucket,_count,_sum}  (histogram)
//   redis_single_errors                          (counter)
//   redis_pipelined_commands                     (counter)
//   redis_pipelined_errors                       (counter)
// No _total suffix — the hook declares names verbatim and client_golang does
// not auto-append.

// redisClientLabel is the label that names the Redis client on the hook's series. The hook
// labels them `instance` (its WithInstanceName option); Prometheus's default
// honor_labels: false keeps the target's own `instance` and stores the hook's value as
// exported_instance. Change it if your scrape config or your hook keeps the client name under
// another label.
const redisClientLabel = "exported_instance"

const (
	clientMatch = redisClientLabel + `=~"$client"`
	cmdBucket   = `redis_single_commands_bucket{job=~"$job", ` + clientMatch + `}`
	cmdCount    = `redis_single_commands_count{job=~"$job", ` + clientMatch + `}`
	cmdErrors   = `redis_single_errors{job=~"$job", ` + clientMatch + `}`
	pipeCmds    = `redis_pipelined_commands{job=~"$job", ` + clientMatch + `}`
	pipeErrors  = `redis_pipelined_errors{job=~"$job", ` + clientMatch + `}`
	clientBy    = `sum by (` + redisClientLabel + `) `
	clientLeg   = `{{` + redisClientLabel + `}}`
)

func redisCommandDashboard() *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder("Redis Command Health").
		Uid("redis-command-health").
		Tags([]string{"redis", "go-redis"}).
		Readonly().
		Tooltip(dashboard.DashboardCursorSyncCrosshair).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser).
		WithVariable(datasourceVariable()).
		WithVariable(jobVariable("redis_single_commands_count")).
		WithVariable(redisClientVariable()).

		// Overview row — the number that should be on the on-call's screen.
		WithRow(dashboard.NewRowBuilder("Overview")).
		WithPanel(
			statPanelWithThresholds(
				"Command Error Rate %",
				"redis_single_errors / redis_single_commands_count. Non-Nil errors only (redis.Nil is filtered by the hook). Spikes here track the Redis server's health, not client pool sizing. 0 when commands ran and none failed, even before any error series exists. No data when no command ran in the window, rather than 0/0.",
				`(sum(rate(`+cmdErrors+`[$__rate_interval])) or vector(0)) / sum(rate(`+cmdCount+`[$__rate_interval])) and sum(rate(`+cmdCount+`[$__rate_interval])) > 0`,
				units.PercentUnit,
				greenYellowRedThresholds(0.001, 0.01),
			).Span(6).Height(4),
		).

		// Command latency / throughput — collapsed.
		WithRow(redisCommandRow()).

		// Errors & failure modes — collapsed.
		WithRow(redisErrorsRow())
}

func redisCommandRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Command Latency & Throughput").
		Collapsed(true).
		WithPanel(
			latencyTimeseries(
				"Command Latency (p50 / p95 / p99) — overall",
				"Across every command and every client. Quantiles cannot exceed the hook's highest histogram bucket — anything slower lands in the +Inf bucket. If p99 sits on that bucket, switch to the per-client panel.",
				cmdBucket,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Command p99 Latency, by client",
				"Per-client p99. Lets you tell whether a latency spike is dashboard-wide (Redis server or network) or one client (a hot key, a slow Lua script).",
				`histogram_quantile(0.99, sum by (le, `+redisClientLabel+`) (rate(`+cmdBucket+`[$__rate_interval])))`,
				clientLeg,
				units.Seconds,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Command Throughput (per sec), by client",
				"Single-command rate via histogram _count (every observation regardless of bucket).",
				clientBy+`(rate(`+cmdCount+`[$__rate_interval]))`,
				clientLeg,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Top 10 Commands by Rate (across clients)",
				"Lets you see whether traffic is GET-heavy, EVAL-heavy or BLPOP-heavy. Useful when a new feature lands and you want to confirm what it's hitting.",
				`topk(10, sum by (command) (rate(`+cmdCount+`[$__rate_interval])))`,
				`{{command}}`,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Pipelined Command Rate, by client",
				"Counts every command inside a pipeline (per-command, not per-pipeline). Useful for sizing: a high share of pipelined commands means fewer round trips.",
				clientBy+`(rate(`+pipeCmds+`[$__rate_interval]))`,
				clientLeg,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		)
}

func redisErrorsRow() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder("Errors & Failure Modes").
		Collapsed(true).
		WithPanel(
			byClientTimeseries(
				"Single Command Errors (rate)",
				"Non-Nil errors. redis.Nil (cache miss) is filtered out by the hook.",
				clientBy+`(rate(`+cmdErrors+`[$__rate_interval]))`,
				clientLeg,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Pipelined Command Errors (rate)",
				"Per-command errors inside pipelines. A pipeline can partially fail — these surface those.",
				clientBy+`(rate(`+pipeErrors+`[$__rate_interval]))`,
				clientLeg,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		).
		WithPanel(
			byClientTimeseries(
				"Top 10 Failing Commands",
				"Which command verb is failing most. EXEC failures usually mean transaction conflict; SET/GET failures with high rate usually point to a Redis server outage.",
				`topk(10, sum by (command) (rate(`+cmdErrors+`[$__rate_interval])))`,
				`{{command}}`,
				units.RequestsPerSecond,
			).Span(12).Height(8),
		)
}

// byClientTimeseries is defaultTimeseries with a caller-supplied unit and legend format, and a
// table legend.
func byClientTimeseries(title, description, expr, legend, unit string) *timeseries.PanelBuilder {
	return defaultTimeseries().
		Title(title).
		Description(description).
		Unit(unit).
		Legend(tableLegend()).
		WithTarget(promQuery(expr, legend))
}

// redisClientVariable — multi-select of every client name (redisClientLabel) seen in
// redis_single_commands_count. "All" resolves to ".+" so the regex matcher
// stays well-formed when nothing is filtered.
func redisClientVariable() cog.Builder[dashboard.VariableModel] {
	return dashboard.NewQueryVariableBuilder("client").
		Label("Redis Client").
		Datasource(datasourceRef()).
		Query(dashboard.StringOrMap{String: cog.ToPtr(`label_values(redis_single_commands_count, ` + redisClientLabel + `)`)}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		IncludeAll(true).
		Multi(true).
		AllValue(".+").
		Current(allSelected())
}
