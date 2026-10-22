// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// The build tag keeps this file out of the repository's own build. run.sh copies it into
// dashboards/ without the tag line; do the same when you add a dashboard of your own.
//go:build example

package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/units"
)

// goRuntimeDashboard reads the Go runtime series that the Prometheus Go client registers by
// default in every instrumented Go program.
func goRuntimeDashboard() *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder("Go Runtime").
		Uid("go-runtime").
		Tags([]string{"go"}).
		Readonly().
		Refresh("30s").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser).
		WithVariable(datasourceVariable()).
		WithVariable(jobVariable("go_goroutines")).
		WithRow(dashboard.NewRowBuilder("Runtime")).
		WithPanel(
			defaultTimeseries().
				Title("Goroutines").
				Description("Live goroutines per process. A line that only climbs is a goroutine leak.").
				Unit(units.Short).
				WithTarget(promQuery(`go_goroutines{job=~"$job"}`, "{{instance}}")).
				Span(12).Height(8),
		).
		WithPanel(
			defaultTimeseries().
				Title("Heap In Use").
				Description("Bytes of heap objects the garbage collector has not freed yet, per process.").
				Unit(units.BytesIEC).
				WithTarget(promQuery(`go_memstats_heap_alloc_bytes{job=~"$job"}`, "{{instance}}")).
				Span(12).Height(8),
		)
}
