package pod_traffic

import (
	"fmt"
	"strings"

	"github.com/netdata/go-orchestrator/module"
)

type (
	// Charts is an alias for modules.Charts
	Charts = module.Charts
	// Dims is an alias for modules.Dims
	Dim  = module.Dim
	Dims = module.Dims
)

var vfTrafficCharts = Charts{
	{
		ID:    "%s-%s", // portName, vfIndex
		Title: "VF Traffic",
		Units: "kilobits/s",
		Type:  module.Area,
		Fam:   "VFTraffic",
		Dims: Dims{
			{ID: "%s-%s-Send", Name: "Send", Algo: module.Incremental, Mul: 8, Div: -1024},
			{ID: "%s-%s-Recv", Name: "Recv", Algo: module.Incremental, Mul: 8, Div: 1024},
		},
	},
	{
		ID:    "RDMA-%s-%s", // portName, vfIndex
		Title: "VF RDMA",
		Units: "kilobits/s",
		Type:  module.Area,
		Fam:   "VFRDMA",
		Dims: Dims{
			{ID: "%s-%s-RDMA-Send", Name: "Send", Algo: module.Incremental, Mul: 4 * 8, Div: -1024},
			{ID: "%s-%s-RDMA-Recv", Name: "Recv", Algo: module.Incremental, Mul: 4 * 8, Div: 1024},
		},
	},
}

var vethTrafficCharts = Charts{
	{
		ID:    "%s", // portName
		Title: "VethPair Traffic",
		Units: "kilobits/s",
		Type:  module.Area,
		Fam:   "VethPair",
		Dims: Dims{
			{ID: "%s-Send", Name: "Send", Algo: module.Incremental, Mul: 8, Div: -1024}, // portName
			{ID: "%s-Recv", Name: "Recv", Algo: module.Incremental, Mul: 8, Div: 1024},  // portName
		},
	},
}

var vfCountCharts = Charts{
	{
		ID:    "VFCount",
		Title: "VF Count",
		Units: "num",
		Type:  module.Line,
		Fam:   "VFCount",
	},
}

// // name: Rdma-portName-vfIndex-Recv
// func newVFRDMACharts(name string) *Charts {
// names := strings.Split(name, "-")
// if len(names) < 3 {
// return nil
// }
// cs := vfRDMACharts.Copy()
// for _, chart := range *cs {
// chart.ID = fmt.Sprintf(chart.ID, names[1], names[2])
// for _, dim := range chart.Dims {
// dim.ID = fmt.Sprintf(dim.ID, names[1], names[2])
// }
// }
// return cs
// }

// name: portName-vfIndex-Recv
func newVFTrafficCharts(name string) *Charts {
	names := strings.Split(name, "-")
	if len(names) < 2 {
		return nil
	}
	cs := vfTrafficCharts.Copy()
	for _, chart := range *cs {
		chart.ID = fmt.Sprintf(chart.ID, names[0], names[1])
		for _, dim := range chart.Dims {
			dim.ID = fmt.Sprintf(dim.ID, names[0], names[1])
		}
	}
	return cs
}

// name: portName-Recv
func newVethPairTrafficCharts(name string) *Charts {
	names := strings.Split(name, "-")
	cs := vethTrafficCharts.Copy()
	for _, chart := range *cs {
		chart.ID = fmt.Sprintf(chart.ID, names[0])
		for _, dim := range chart.Dims {
			dim.ID = fmt.Sprintf(dim.ID, names[0])
		}
	}
	return cs
}
