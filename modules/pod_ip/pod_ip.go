package pod_ip

import (
	"io/ioutil"

	"github.com/netdata/go-orchestrator/logger"
	"github.com/netdata/go-orchestrator/module"
)

func init() {
	creator := module.Creator{
		Create: func() module.Module { return New() },
	}
	creator.Defaults.Disabled = true
	module.Register("pod_ip", creator)
}

func New() *PodIP {
	return &PodIP{
		metrics: make(map[string]int64),
	}
}

type PodIP struct {
	module.Base // should be embedded by every module
	metrics     map[string]int64
	Dir         string `yaml:"dir"`
}

// Cleanup makes cleanup
func (pi *PodIP) Cleanup() {
}

// Init makes initialization
func (pi *PodIP) Init() bool {
	if pi.Dir == "" {
		return false
	}
	err := pi.ReadIPDir()
	if err != nil {
		logger.Errorln(err)
		return false
	}
	return true
}

// Check makes check
func (pi *PodIP) Check() bool {
	return true
}

// Charts creates Charts
func (pi *PodIP) Charts() *module.Charts {
	return podIPCharts.Copy()
}

// Collect collects metrics
func (pi *PodIP) Collect() map[string]int64 {
	err := pi.ReadIPDir()
	if err != nil {
		logger.Errorln(err)
	}
	return pi.metrics
}

func (pi *PodIP) ReadIPDir() error {
	files, err := ioutil.ReadDir(pi.Dir)
	if err != nil {
		return err
	}
	pi.metrics["podIP"] = int64(len(files) - 2)
	return nil
}
