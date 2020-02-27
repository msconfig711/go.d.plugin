package pod_ip

import (
	"github.com/netdata/go-orchestrator/module"
)

type (
	// Charts is an alias for modules.Charts
	Charts = module.Charts
	// Dims is an alias for modules.Dims
	Dim  = module.Dim
	Dims = module.Dims
)

var podIPCharts = Charts{
	{
		ID:    "podIP",
		Title: "pod ip num",
		Units: "num",
		Type:  module.Line,
		Dims: Dims{
			{ID: "podIP", Name: ""},
		},
	},
}
