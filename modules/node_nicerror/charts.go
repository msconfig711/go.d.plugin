package node_nicerror

import "github.com/netdata/go-orchestrator/module"

type (
	// Charts is an alias for modules.Charts
	Charts = module.Charts
	// Dims is an alias for modules.Dims
	Dim = module.Dim
)

var charts = Charts{
	{
		ID:    "NICError",
		Title: "Netcard error counter",
		Units: "num",
		Fam:   "NICError",
	},
}
