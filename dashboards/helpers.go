// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT AND Apache-2.0
//
// defaultTimeseries, allSelected and the query, variable and stat helpers are adapted, with
// changes, from the linux-node-overview example of the Grafana Foundation SDK
// (https://github.com/grafana/grafana-foundation-sdk), licensed under Apache-2.0; see
// third_party/grafana-foundation-sdk/LICENSE.

package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
	"github.com/grafana/grafana-foundation-sdk/go/units"
)

func datasourceRef() common.DataSourceRef {
	return common.DataSourceRef{
		Uid:  cog.ToPtr("${datasource}"),
		Type: cog.ToPtr("prometheus"),
	}
}

func datasourceVariable() cog.Builder[dashboard.VariableModel] {
	return dashboard.NewDatasourceVariableBuilder("datasource").
		Label("Data source").
		Type("prometheus")
}

// jobVariable is the Job dropdown, filled with the jobs that export metric, so it lists only the
// jobs that have data for the dashboard rather than every scrape job.
func jobVariable(metric string) cog.Builder[dashboard.VariableModel] {
	return dashboard.NewQueryVariableBuilder("job").
		Label("Job").
		Datasource(datasourceRef()).
		Query(dashboard.StringOrMap{String: cog.ToPtr("label_values(" + metric + ", job)")}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		IncludeAll(true).
		Multi(true).
		AllValue(".+").
		Current(allSelected())
}

// allSelected is the option a multi-select variable starts on: every value.
func allSelected() dashboard.VariableOption {
	return dashboard.VariableOption{
		Selected: cog.ToPtr(true),
		Text:     dashboard.StringOrArrayOfString{ArrayOfString: []string{"All"}},
		Value:    dashboard.StringOrArrayOfString{ArrayOfString: []string{"$__all"}},
	}
}

func promQuery(expr, legendFormat string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expr).
		LegendFormat(legendFormat).
		Datasource(datasourceRef())
}

func promInstantQuery(expr string) *prometheus.DataqueryBuilder {
	return promQuery(expr, "").Instant()
}

func statPanel(title, description, expr, unit string) *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title(title).
		Description(description).
		Datasource(datasourceRef()).
		Unit(unit).
		Decimals(2).
		WithTarget(promInstantQuery(expr)).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

func statPanelWithThresholds(title, description, expr, unit string, thresholds cog.Builder[dashboard.ThresholdsConfig]) *stat.PanelBuilder {
	return statPanel(title, description, expr, unit).Thresholds(thresholds)
}

// countStat is a stat for a whole count, such as tasks or jobs: no decimals and no unit, because
// the short unit scales by 1,000 and with no decimals would print 2706 as "3 K".
func countStat(title, description, expr string, thresholds cog.Builder[dashboard.ThresholdsConfig]) *stat.PanelBuilder {
	return statPanelWithThresholds(title, description, expr, units.NoUnit, thresholds).Decimals(0)
}

func defaultTimeseries() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Datasource(datasourceRef()).
		LineWidth(1).
		FillOpacity(15).
		ShowPoints(common.VisibilityModeNever).
		DrawStyle(common.GraphDrawStyleLine).
		GradientMode(common.GraphGradientModeOpacity).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		LineInterpolation(common.LineInterpolationSmooth).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode(common.LegendDisplayModeList).
			Placement(common.LegendPlacementBottom).
			ShowLegend(true),
		).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode(common.TooltipDisplayModeMulti).
			Sort(common.SortOrderDescending),
		)
}

func latencyTimeseries(title, description, bucketExpr string) *timeseries.PanelBuilder {
	p := defaultTimeseries().
		Title(title).
		Description(description).
		Unit(units.Seconds)

	for _, q := range []struct {
		quantile string
		legend   string
	}{
		{"0.5", "p50"},
		{"0.95", "p95"},
		{"0.99", "p99"},
	} {
		expr := "histogram_quantile(" + q.quantile + ", sum(rate(" + bucketExpr + "[$__rate_interval])) by (le))"
		p = p.WithTarget(promQuery(expr, q.legend))
	}

	return p
}

func greenYellowRedThresholds(yellow, red float64) cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "green"},
			{Value: cog.ToPtr(yellow), Color: "yellow"},
			{Value: cog.ToPtr(red), Color: "red"},
		})
}

// greenRedThresholds is two steps, green base and red at and above red, for a count whose only
// healthy reading is 0.
func greenRedThresholds(red float64) cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "green"},
			{Value: cog.ToPtr(red), Color: "red"},
		})
}

// neutralThresholds is a single base step in a colour that carries no health judgement, for a
// stat such as throughput where neither a high nor a low reading is bad on its own.
func neutralThresholds() cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "blue"},
		})
}

// Grafana's names for the reducers a table legend shows: minimum, mean, maximum and the last
// non-null value.
const (
	reduceMin         = "min"
	reduceMean        = "mean"
	reduceMax         = "max"
	reduceLastNotNull = "lastNotNull"
)

// tableLegend lists the minimum, mean, maximum and last value of each series in a table, sorted
// by the last value descending, so the hottest series shows without hovering.
func tableLegend() *common.VizLegendOptionsBuilder {
	return common.NewVizLegendOptionsBuilder().
		DisplayMode(common.LegendDisplayModeTable).
		Placement(common.LegendPlacementBottom).
		ShowLegend(true).
		Calcs([]string{reduceMin, reduceMean, reduceMax, reduceLastNotNull}).
		SortBy("Last *").
		SortDesc(true)
}
