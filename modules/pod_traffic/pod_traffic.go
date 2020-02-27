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
		metrics:    make(map[string]int64),
		mutex:      sync.Mutex{},
		ethHandler: ethHandler,
		watcher:    watcher,
		preMap:     make(map[string]string),
	}
}

type PodTraffic struct {
	module.Base // should be embedded by every module
	metrics     map[string]int64
	mutex       sync.Mutex

	ethHandler *ethtool.Ethtool
	watcher    *fsnotify.Watcher // watch the checkpoint file. update the chart and metric map when check point file changed

	podMap  map[string]PodInfo
	preMap  map[string]string // use to update the chart
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
	pt.preMap = make(map[string]string)
	for key, podInfo := range pt.podMap {
		if podInfo.VfIndex == -1 {
			pt.preMap[key] = podInfo.HostVeth
		} else {
			pt.preMap[key] = podInfo.HostVeth + "-" + strconv.Itoa(podInfo.VfIndex)
		}
	}
	go pt.WatchCheckPoint()
	if err := pt.getTraffic(); err != nil {
		logger.Errorln(err)
		return false
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
	pt.mutex.Lock()
	defer pt.mutex.Unlock()
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
	var success bool
	var retry = 10
	var count int
	for !success && count < retry {
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
					count++
					continue
				}
				success = true
			}
		}
	}
	if !success {
		logger.Errorln("add chart failed after retry 10 times!")
	}
	for key, chartID := range pt.preMap {
		if _, exists := pt.podMap[key]; !exists {
			pt.mutex.Lock()
			delete(pt.metrics, chartID+"-Recv")
			delete(pt.metrics, chartID+"-Send")
			chartID = strings.Replace(chartID, "-", "_", -1)
			(*pt.dynamicCharts).Get(chartID).MarkRemove()
			(*pt.dynamicCharts).Get(chartID).MarkNotCreated()
			pt.mutex.Unlock()
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
	for key, _ := range pt.metrics {
		if key == "vfCount" {
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
		} else {
			if len(strings.Split(key, "-")) == 3 {
				err := PodTrafficCharts.Add(*newVFTrafficCharts(key)...)
				if err != nil {
					logger.Errorln("Error when add vf traffic chart:", err)
				}
			} else {
				err := PodTrafficCharts.Add(*newVethPairTrafficCharts(key)...)
				if err != nil {
					logger.Errorln("Error when add veth pair traffic chart:", err)
				}
			}
		}
	}
	pt.dynamicCharts = PodTrafficCharts
	return PodTrafficCharts
}

// Collect collects metrics
func (pt *PodTraffic) Collect() map[string]int64 {
	err := pt.getTraffic()
	if err != nil {
		logger.Errorln("Error when get traffic: ", err)
		return nil
	}
	return pt.metrics
}

func (pt *PodTraffic) getTraffic() error {
	var outterErr error
	var wg sync.WaitGroup
	for _, podInfo := range pt.podMap {
		wg.Add(1)
		if podInfo.VfIndex == -1 {
			go func(portName string) {
				defer wg.Done()
				var recvPrefix = portName + "-Recv"
				var sendPrefix = portName + "-Send"
				pt.mutex.Lock()
				defer pt.mutex.Unlock()
				cmdStr := fmt.Sprintf("cat /sys/class/net/%s/statistics/rx_bytes", portName)
				data := pt.Exec(cmdStr)
				if data == "" {
					return
				}
				count, err := strconv.Atoi(strings.TrimSpace(strings.Trim(data, "\n")))
				if err != nil {
					outterErr = err
					return
				}
				pt.metrics[sendPrefix] = int64(count)

				// handle the send
				cmdStr = fmt.Sprintf("cat /sys/class/net/%s/statistics/tx_bytes", portName)
				data = pt.Exec(cmdStr)
				if data == "" {
					return
				}
				count, err = strconv.Atoi(strings.TrimSpace(strings.Trim(data, "\n")))
				if err != nil {
					outterErr = err
					return
				}

				pt.metrics[recvPrefix] = int64(count)
			}(podInfo.HostVeth)
		} else {
			go func(portName string, vfIndex int) {
				defer wg.Done()
				stats, err := pt.ethHandler.Stats(portName)
				if err != nil {
					outterErr = err
					return
				}
				pt.mutex.Lock()
				defer pt.mutex.Unlock()
				var prefix = portName + "-" + strconv.Itoa(vfIndex) + "-"
				pt.metrics[prefix+"Recv"] = int64(stats["vport_tx_bytes"])
				pt.metrics[prefix+"Send"] = int64(stats["vport_rx_bytes"])
			}(podInfo.HostVeth, podInfo.VfIndex)
		}
	}
	wg.Wait()
	pt.metrics["vfCount"] = int64(pt.vfCount)
	return outterErr
}

func (pt *PodTraffic) Exec(cmdStr string) string {
	cmd := exec.Command("/bin/bash", "-c", cmdStr)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		logger.Errorf("Error when execute the command %s. Error info is: %v\n", cmdStr, err)
		return ""
	}
	return out.String()
}
