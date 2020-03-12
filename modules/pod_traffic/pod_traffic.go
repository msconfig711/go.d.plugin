package pod_traffic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/msconfig711/ethtool"
	"github.com/netdata/go-orchestrator/logger"
	"github.com/netdata/go-orchestrator/module"
)

func init() {
	creator := module.Creator{
		Create: func() module.Module { return New() },
	}
	creator.Defaults.Disabled = true
	module.Register("pod_traffic", creator)
}

type PodInfo struct {
	PodId        string
	HostVeth     string
	IP           string
	MAC          string
	VNI          int
	VfIndex      int
	MaxBandwidth int
	MinBandwidth int
}

func New() *PodTraffic {
	ethHandler, err := ethtool.NewEthtool()
	if err != nil {
		logger.Errorln("error when init ethtool handler! ", err)
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorln("error when init watcher handler! ", err)
	}
	return &PodTraffic{
		metrics:     make(map[string]int64),
		metricMutex: sync.Mutex{},
		mapMutex:    sync.Mutex{},
		ethHandler:  ethHandler,
		watcher:     watcher,
		preMap:      make(map[string]string),
	}
}

type PodTraffic struct {
	module.Base // should be embedded by every module
	metrics     map[string]int64
	metricMutex sync.Mutex
	mapMutex    sync.Mutex

	ethHandler *ethtool.Ethtool
	watcher    *fsnotify.Watcher // watch the checkpoint file. update the chart and metric map when check point file changed

	podMap  map[string]PodInfo
	preMap  map[string]string // use to update the chart
	vfMap   map[int]string    // key:vfIndex, value: mlx5_xxxx
	vfCount int

	CheckPoint     string `yaml:"check_point"`
	UpdateInterval int    `yaml:"update_every"`

	dynamicCharts *module.Charts
}

// Cleanup makes cleanup
func (pt *PodTraffic) Cleanup() {
	defer pt.ethHandler.Close()
	defer pt.watcher.Close()
}

// Init makes initialization
func (pt *PodTraffic) Init() bool {
	err := pt.ReadCheckPoint()
	if err != nil {
		logger.Errorln("error when read the checkpoint file:", err)
		return false
	}
	if !pt.getVFDir() {
		return false
	}
	pt.preMap = make(map[string]string)
	for key, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			pt.preMap[key] = podInfo.HostVeth
		} else {
			pt.preMap[key] = podInfo.HostVeth + "-" + strconv.Itoa(podInfo.VfIndex)
		}
	}
	go pt.WatchCheckPoint()
	return true
}

func (pt *PodTraffic) getVFDir() bool {
	pt.vfMap = make(map[int]string)
	for _, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			continue
		}
		cmdStr := fmt.Sprintf("lspci|grep Mell|grep Virtual|sed -n '%dp'|awk '{print $1}'", podInfo.VfIndex+1)
		pciAddr, err := pt.Exec(cmdStr)
		if err != nil {
			return false
		}
		cmdStr = fmt.Sprintf("ls /sys/class/infiniband/ -alh|grep \"%s\"|awk '{print $9}'", strings.TrimSpace(pciAddr))
		dirName, err := pt.Exec(cmdStr)
		if err != nil {
			return false
		}
		pt.vfMap[podInfo.VfIndex] = fmt.Sprintf("cat /sys/class/infiniband/%s/ports/1/counters/", strings.TrimSpace(dirName))
	}
	return true
}

func (pt *PodTraffic) ReadCheckPoint() error {
	_, err := os.Stat(pt.CheckPoint)
	if err != nil {
		return err
	}
	data, err := ioutil.ReadFile(pt.CheckPoint)
	if err != nil {
		return err
	}
	pt.mapMutex.Lock()
	defer pt.mapMutex.Unlock()
	pt.podMap = make(map[string]PodInfo)
	err = json.Unmarshal(data, &pt.podMap)
	if err != nil {
		return err
	}
	pt.vfCount = 0
	for _, podInfo := range pt.podMap {
		if podInfo.VfIndex != -1 {
			pt.vfCount++
		}
	}
	return nil
}

func (pt *PodTraffic) RefreshCharts() {
	for key, podInfo := range pt.podMap {
		name := podInfo.HostVeth + "-" + strconv.Itoa(podInfo.VfIndex)
		if _, exists := pt.preMap[key]; !exists {
			var err error
			if podInfo.VfIndex == -1 {
				err = pt.dynamicCharts.Add(*newVethPairTrafficCharts(name)...)
			} else {
				err = pt.dynamicCharts.Add(*newVFTrafficCharts(name)...)
			}
			if err != nil {
				logger.Errorln("error when add the chart:", name, err)
			}
		}
	}
	for key, chartID := range pt.preMap {
		if _, exists := pt.podMap[key]; !exists {
			pt.metricMutex.Lock()
			delete(pt.metrics, chartID+"-Recv")
			delete(pt.metrics, chartID+"-Send")
			pt.metricMutex.Unlock()
			// chartID = strings.Replace(chartID, "-", "_", -1)
			chart := (*pt.dynamicCharts).Get(chartID)
			if chart != nil {
				chart.MarkRemove()
				chart.MarkNotCreated()
			}
			if len(strings.Split(chartID, "-")) > 1 {
				chart := (*pt.dynamicCharts).Get("RDMA-" + chartID)
				if chart != nil {
					chart.MarkRemove()
					chart.MarkNotCreated()
				}
			}
		}
	}
	pt.preMap = make(map[string]string)
	for key, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			pt.preMap[key] = podInfo.HostVeth
		} else {
			pt.preMap[key] = podInfo.HostVeth + "-" + strconv.Itoa(podInfo.VfIndex)
		}
	}
}

func (pt *PodTraffic) WatchCheckPoint() {
	err := pt.watcher.Add(pt.CheckPoint)
	if err != nil {
		logger.Panicf("error when watch the check point file!,%v", err)
	}
	for {
		select {
		case ev := <-pt.watcher.Events:
			{
				if ev.Op&fsnotify.Write == fsnotify.Write {
					logger.Infoln("the file has been updated!")
					pt.ReadCheckPoint()
					pt.getVFDir()
					pt.RefreshCharts()
				}
			}
		case err := <-pt.watcher.Errors:
			{
				logger.Errorln("error when watch the check point file:", err)
				return
			}
		}
	}
}

// Check makes check
func (pt *PodTraffic) Check() bool {
	return true
}

// Charts creates Charts
func (pt *PodTraffic) Charts() *module.Charts {
	PodTrafficCharts := &Charts{}
	pt.mapMutex.Lock()
	defer pt.mapMutex.Unlock()
	for _, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			err := PodTrafficCharts.Add(*newVethPairTrafficCharts(podInfo.HostVeth)...)
			if err != nil {
				logger.Errorln("Error when add veth pair traffic chart:", err)
			}
		} else {
			key := podInfo.HostVeth + "-" + strconv.Itoa(podInfo.VfIndex)
			err := PodTrafficCharts.Add(*newVFTrafficCharts(key)...)
			if err != nil {
				logger.Errorln("Error when add vf traffic chart:", err)

			}
		}
	}
	// add vf count chart
	vfch := vfCountCharts.Copy()
	dim := &Dim{ID: "vfCount", Name: "VF Count"}
	if err := vfch.Get("VFCount").AddDim(dim); err != nil {
		logger.Errorln("Error when create chart for vf:", err)
		return nil
	}
	err := PodTrafficCharts.Add(*vfch...)
	if err != nil {
		logger.Errorln("Error when add vf charts:", err)
	}
	pt.dynamicCharts = PodTrafficCharts
	return PodTrafficCharts
}

// Collect collects metrics
func (pt *PodTraffic) Collect() map[string]int64 {
	pt.mapMutex.Lock()
	defer pt.mapMutex.Unlock()
	for _, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			go pt.GetVethTraffic(podInfo.HostVeth)
		} else {
			go pt.GetVfTraffic(podInfo.HostVeth, podInfo.VfIndex)
		}
	}
	// err := pt.getTraffic()
	// if err != nil {
	// logger.Errorln("Error when get traffic: ", err)
	// return nil
	// }
	pt.metrics["vfCount"] = int64(pt.vfCount)
	return pt.metrics
}

func (pt *PodTraffic) GetVethTraffic(portName string) {
	var recvPrefix = portName + "-Recv"
	var sendPrefix = portName + "-Send"
	pt.metricMutex.Lock()
	defer pt.metricMutex.Unlock()
	cmdStr := fmt.Sprintf("cat /sys/class/net/%s/statistics/rx_bytes", portName)
	data, err := pt.Exec(cmdStr)
	if err != nil {
		logger.Errorf("Error when read the counter for veth %s:%s", portName, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.Trim(data, "\n")))
	if err != nil {
		logger.Errorf("Error when convert the counter for veth %s:%s", portName, err)
	}
	pt.metrics[sendPrefix] = int64(count)

	// handle the send
	cmdStr = fmt.Sprintf("cat /sys/class/net/%s/statistics/tx_bytes", portName)
	data, err = pt.Exec(cmdStr)
	if err != nil {
		logger.Errorf("Error when read the counter for veth %s:%s", portName, err)
	}
	count, err = strconv.Atoi(strings.TrimSpace(strings.Trim(data, "\n")))
	if err != nil {
		logger.Errorf("Error when convert the counter for veth %s:%s", portName, err)
	}

	pt.metrics[recvPrefix] = int64(count)
}

func (pt *PodTraffic) GetVfTraffic(portName string, vfIndex int) {
	go func(portName string, vfIndex int) {
		stats, err := pt.ethHandler.Stats(portName)
		if err != nil {
			logger.Errorf("Error when get stats for port:%s by ethtool", portName)
		}
		pt.metricMutex.Lock()
		defer pt.metricMutex.Unlock()
		var prefix = portName + "-" + strconv.Itoa(vfIndex) + "-"
		pt.metrics[prefix+"Recv"] = int64(stats["vport_tx_bytes"])
		pt.metrics[prefix+"Send"] = int64(stats["vport_rx_bytes"])
	}(portName, vfIndex)
	go func(portName string, vfIndex int) {
		var prefix = portName + "-" + strconv.Itoa(vfIndex) + "-" + "RDMA-"
		metric, err := pt.Exec(pt.vfMap[vfIndex] + "port_rcv_data")
		if err != nil {
			logger.Errorf("Error when get rdma counter for port:%s", portName)
			return
		}
		data, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(metric, "\n")))
		if err != nil {
			logger.Errorf("Error when convert rdma counter for port:%s", portName)
			return
		}
		pt.metricMutex.Lock()
		defer pt.metricMutex.Unlock()
		pt.metrics[prefix+"Recv"] = int64(data)
		metric, err = pt.Exec(pt.vfMap[vfIndex] + "port_xmit_data")
		if err != nil {
			logger.Errorf("Error when get rdma counter for port:%s", portName)
			return
		}
		data, err = strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(metric, "\n")))
		if err != nil {
			logger.Errorf("Error when convert rdma counter for port:%s", portName)
			return
		}
		pt.metrics[prefix+"Send"] = int64(data)
	}(portName, vfIndex)
}

// func (pt *PodTraffic) getTraffic() error {
// var outterErr error
// for _, podInfo := range pt.podMap {
// if podInfo.VfIndex == -1 {
// go func(portName string) {
// }(podInfo.HostVeth)
// } else {
// }
// }
// return outterErr
// }

func (pt *PodTraffic) Exec(cmdStr string) (string, error) {
	cmd := exec.Command("/bin/bash", "-c", cmdStr)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		logger.Errorf("Error when execute the command %s. Error info is: %v\n", cmdStr, err)
		return "", err
	}
	return out.String(), nil
}
