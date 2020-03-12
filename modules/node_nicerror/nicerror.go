package node_nicerror

import (
	"github.com/msconfig711/ethtool"
	"github.com/netdata/go-orchestrator/module"
	"log"
	"regexp"
)

func init() {
	creator := module.Creator{
		Create: func() module.Module { return New() },
	}
	creator.Defaults.Disabled = true
	module.Register("node_nicerror", creator)
}

// New creates rxpackets with default values
func New() *NE {
	ethHandler, err := ethtool.NewEthtool()
	if err != nil {
		log.Println("Error when init ethtool handler! ", err)
		return nil
	}
	return &NE{
		metrics:    make(map[string]int64),
		counterMap: make(map[string]int64),
		ethHandler: ethHandler,
	}
}

type NE struct {
	module.Base   // should be embedded by every module
	metrics       map[string]int64
	counterMap    map[string]int64
	ethHandler    *ethtool.Ethtool
	InterfaceName string `yaml:"interface_name"`
}

// Cleanup makes cleanup
func (ne *NE) Cleanup() {
	ne.ethHandler.Close()
}

// Init makes initialization
func (ne *NE) Init() bool {
	if err := ne.setup(); err != nil {
		log.Println(err)
		return false
	}
	return true
}

// Check makes check
func (ne *NE) Check() bool {
	return true
}

// Charts creates Charts
func (ne *NE) Charts() *module.Charts {
	charts := charts.Copy()
	for rxName, _ := range ne.metrics {
		chart := charts.Get("NICError")
		dim := &Dim{ID: rxName, Name: rxName, Div: 1}
		if err := chart.AddDim(dim); err != nil {
			return nil
		}
	}
	return charts
}

// Collect collects metrics
func (ne *NE) Collect() map[string]int64 {
	err := ne.getPackets(false)
	if err != nil {
		log.Println(err)
		return nil
	}
	return ne.metrics
}

func (ne *NE) setup() error {
	return ne.getPackets(true)
}

func (ne *NE) getPackets(isSetup bool) error {
	re := regexp.MustCompile(`((r|t)x(_queue)*[\d]{0,2}_(stopped|dropped|wake|wqe_err|congst_umr|buff_alloc_err|steer_missed_packets|symbol_error_phy|unsupported_op_phy|corrected_bits_phy|in_range_len_errors_phy|out_of_range_len_phy|oversize_pkts_phy|discards_phy|errors_phy|undersize_pkts_phy|fragments_phy|jabbers_phy|out_of_buffer|pause_storm_errors_events|pcs_symbol_err_phy))`)
	stats, err := ne.ethHandler.Stats(ne.InterfaceName)
	if err != nil {
		return err
	}
	for k, v := range stats {
		if re.MatchString(k) || k == "link_down_events_phy" {
			if isSetup {
				ne.metrics[k] = 0
				ne.counterMap[k] = int64(v)
			} else {
				ne.metrics[k] = int64(v) - ne.counterMap[k]
				ne.counterMap[k] = int64(v)
			}
		}
	}
	return nil
}
