package pod_ping

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/netdata/go-orchestrator/logger"
	"github.com/netdata/go-orchestrator/module"
	"github.com/sparrc/go-ping"
)

func init() {
	creator := module.Creator{
		Create: func() module.Module { return New() },
	}
	creator.Defaults.Disabled = true
	module.Register("pod_ping", creator)
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

func New() *PodPing {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorln("Error when init watcher handler! ", err)
	}
	return &PodPing{
		metrics: make(map[string]int64),
		watcher: watcher,
		mutex:   sync.Mutex{},
	}
}

type PodPing struct {
	module.Base // should be embedded by every module
	metrics     map[string]int64
	CheckPoint  string `yaml:"check_point"`

	mutex         sync.Mutex
	watcher       *fsnotify.Watcher
	nodeIP        []string
	hostIP        string // the ip address of the host0
	podMap        map[string]PodInfo
	preMap        map[string]string // value: pod ip
	dynamicCharts *module.Charts
}

// Cleanup makes cleanup
func (pp *PodPing) Cleanup() {
	defer pp.watcher.Close()
}

// Init makes initialization
func (pp *PodPing) Init() bool {
	pp.ReadCheckPoint()
	go pp.WatchCheckPoint()
	err := pp.getNodeIP()
	if err != nil {
		return false
	}
	err = pp.getHostIP()
	if err != nil {
		return false
	}
	pp.preMap = make(map[string]string)
	for key, podInfo := range pp.podMap {
		pp.preMap[key] = podInfo.IP
	}
	return true
}

func (pp *PodPing) ReadCheckPoint() {
	_, err := os.Stat(pp.CheckPoint)
	if err != nil {
		logger.Errorln(err)
		return
	}
	data, err := ioutil.ReadFile(pp.CheckPoint)
	if err != nil {
		logger.Errorln("read checkoutpoint file failed", err)
		return
	}
	pp.podMap = make(map[string]PodInfo)
	err = json.Unmarshal(data, &pp.podMap)
	if err != nil {
		logger.Errorln("error when unmarshal the checkpoint file!")
		return
	}
}

func (pp *PodPing) RefreshCharts() {
	for key, podInfo := range pp.podMap {
		if _, exists := pp.preMap[key]; !exists {
			err := pp.dynamicCharts.Add(*newPingLossCharts("pod", podInfo.IP)...)
			if err != nil {
				logger.Errorln("Error when add the ping loss chart:", podInfo.IP, err)
			}
		}
	}
	for key, ip := range pp.preMap {
		if _, exists := pp.podMap[key]; !exists {
			pp.mutex.Lock()
			ip = strings.Replace(ip, ".", "_", -1)
			delete(pp.metrics, ip+"-loss")
			delete(pp.metrics, ip+"-maximum")
			delete(pp.metrics, ip+"-minimum")
			delete(pp.metrics, ip+"-average")
			(*pp.dynamicCharts).Get(ip + "-loss").MarkRemove()
			(*pp.dynamicCharts).Get(ip + "-loss").MarkNotCreated()
			(*pp.dynamicCharts).Get(ip + "-latency").MarkRemove()
			(*pp.dynamicCharts).Get(ip + "-latency").MarkNotCreated()
			pp.mutex.Unlock()
		}
	}

	pp.preMap = make(map[string]string)
	for key, podInfo := range pp.podMap {
		pp.preMap[key] = podInfo.IP
	}
}

func (pp *PodPing) WatchCheckPoint() {
	err := pp.watcher.Add(pp.CheckPoint)
	if err != nil {
		logger.Panicf("Error when watch the check point file!,%v", err)
	}
	for {
		select {
		case ev := <-pp.watcher.Events:
			{
				if ev.Op&fsnotify.Write == fsnotify.Write {
					logger.Errorln("The file has been updated!")
					pp.ReadCheckPoint()
					pp.RefreshCharts()
				}
			}
		case err := <-pp.watcher.Errors:
			{
				logger.Errorln("error : ", err)
				return
			}
		}
	}
}

// Check makes check
func (pp *PodPing) Check() bool {
	return true
}

// Charts creates Charts
func (pp *PodPing) Charts() *module.Charts {
	charts := &Charts{}
	pp.mutex.Lock()
	defer pp.mutex.Unlock()
	for _, podInfo := range pp.podMap {
		ip := strings.Replace(podInfo.IP, ".", "_", -1)
		charts.Add(*newPingLossCharts("pod", ip)...)
		charts.Add(*newPingLatencyCharts("pod", ip)...)
	}
	for _, nodeIP := range pp.nodeIP {
		nodeIP := strings.Replace(nodeIP, ".", "_", -1)
		charts.Add(*newPingLossCharts("node", nodeIP)...)
		charts.Add(*newPingLatencyCharts("node", nodeIP)...)
	}
	pp.dynamicCharts = charts
	return charts
}

// Collect collects metrics
func (pp *PodPing) Collect() map[string]int64 {
	for _, podInfo := range pp.podMap {
		go pp.ping(podInfo.IP)
	}
	for _, nodeIP := range pp.nodeIP {
		go pp.ping(nodeIP)
	}
	return pp.metrics
}

func (pp *PodPing) getNodeIP() error {
	pp.nodeIP = make([]string, 0)
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		pp.nodeIP = append(pp.nodeIP, ip.String())
	}
	return nil
}

// get the ip address of the host0
func (pp *PodPing) getHostIP() error {
	port, err := net.InterfaceByName("host0")
	if err != nil {
		return err
	}
	addrs, err := port.Addrs()
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("The ip is nil")
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return err
		}
		if ip.To4() != nil {
			pp.hostIP = ip.String()
		}
	}
	if pp.hostIP == "" {
		return fmt.Errorf("Error when get the ip of host0")
	}
	return nil
}

func (pp *PodPing) ping(ip string) {
	metricKey := strings.Replace(ip, ".", "_", -1)
	pp.mutex.Lock()
	defer pp.mutex.Unlock()
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		return
	}
	pinger.Source = pp.hostIP
	pinger.Count = 5
	pinger.Timeout = time.Duration(pinger.Count) * 2 * time.Second
	pinger.OnFinish = func(stats *ping.Statistics) {
		pp.metrics[metricKey+"-loss"] = int64(100 - stats.PacketLoss*100)
		pp.metrics[metricKey+"-maximum"] = int64(float64(stats.MaxRtt) / float64(time.Millisecond) * 1000)
		pp.metrics[metricKey+"-minimum"] = int64(float64(stats.MinRtt) / float64(time.Millisecond) * 1000)
		pp.metrics[metricKey+"-average"] = int64(float64(stats.AvgRtt) / float64(time.Millisecond) * 1000)
	}
	pinger.Run() // blocks until finished
}
