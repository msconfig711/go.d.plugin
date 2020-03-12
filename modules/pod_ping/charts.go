package pod_ping

import (
	"fmt"

	"github.com/netdata/go-orchestrator/module"
)

type (
	// Charts is an alias for modules.Charts
	Charts = module.Charts
	// Dims is an alias for modules.Dims
	Dim  = module.Dim
	Dims = module.Dims
)

var pingLossCharts = Charts{
	{
		ID:    "%s-loss", // ip
		Title: "Ping Loss",
		Units: "percentage",
		Type:  module.Area,
		Fam:   "%s_%s_Ping", // kind_ip_ping(kind:node, pod)
		Dims: Dims{
			{ID: "%s-loss", Name: "Loss Percentage"}, // ip
		},
	},
}

var pingLatencyCharts = Charts{
	{
		ID:    "%s-latency",
		Title: "Ping Latency",
		Units: "ms",
		Type:  module.Area,
		Fam:   "%s_%s_Ping",
		Dims: Dims{
			{ID: "%s-maximum", Name: "Maximum", Div: 1000},
			{ID: "%s-minimum", Name: "Minimum", Div: 1000},
			{ID: "%s-average", Name: "Average", Div: 1000},
		},
	},
}

func newPingLossCharts(kind, ip string) *Charts {
	cs := pingLossCharts.Copy()
	for _, chart := range *cs {
		chart.ID = fmt.Sprintf(chart.ID, ip)
		chart.Fam = fmt.Sprintf(chart.Fam, kind, ip)
		for _, dim := range chart.Dims {
			dim.ID = fmt.Sprintf(dim.ID, ip)
		}
	}
	return cs
}

func newPingLatencyCharts(kind, ip string) *Charts {
	cs := pingLatencyCharts.Copy()
	for _, chart := range *cs {
		chart.ID = fmt.Sprintf(chart.ID, ip)
		chart.Fam = fmt.Sprintf(chart.Fam, kind, ip)
		for _, dim := range chart.Dims {
			dim.ID = fmt.Sprintf(dim.ID, ip)
		}
	}
	return cs
}
