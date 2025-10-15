package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type SystemMetrics struct {
	Timestamp    time.Time
	CPUPercent   []float64
	MemoryUsage  *MemoryMetrics
	DiskUsage    *DiskMetrics
	NetworkUsage *NetworkMetrics
	LoadAverage  *LoadMetrics
}

type MemoryMetrics struct {
	Total       uint64
	Available   uint64
	Used        uint64
	UsedPercent float64
	Free        uint64
	Buffers     uint64
	Cached      uint64
}

type DiskMetrics struct {
	ReadBytes  uint64
	WriteBytes uint64
	ReadCount  uint64
	WriteCount uint64
	ReadTime   uint64
	WriteTime  uint64
}

type NetworkMetrics struct {
	BytesSent   uint64
	BytesRecv   uint64
	PacketsSent uint64
	PacketsRecv uint64
	ErrorsIn    uint64
	ErrorsOut   uint64
	DroppedIn   uint64
	DroppedOut  uint64
}

type LoadMetrics struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

type Monitor struct {
	interval    time.Duration
	metrics     []SystemMetrics
	metricsPool *sync.Pool
	mutex       sync.RWMutex
	isRunning   bool
	stopChan    chan struct{}
	initialDisk *disk.IOCountersStat
	initialNet  map[string]net.IOCountersStat
	maxSamples  int
}

func New(interval time.Duration) *Monitor {
	const defaultMaxSamples = 10000
	return &Monitor{
		interval:   interval,
		metrics:    make([]SystemMetrics, 0, 128),
		stopChan:   make(chan struct{}),
		maxSamples: defaultMaxSamples,
		metricsPool: &sync.Pool{
			New: func() interface{} {
				return &SystemMetrics{
					MemoryUsage:  &MemoryMetrics{},
					DiskUsage:    &DiskMetrics{},
					NetworkUsage: &NetworkMetrics{},
					LoadAverage:  &LoadMetrics{},
				}
			},
		},
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	m.mutex.Lock()
	if m.isRunning {
		m.mutex.Unlock()
		return nil
	}
	m.isRunning = true
	m.mutex.Unlock()

	if err := m.initializeBaselines(); err != nil {
		return err
	}

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.stopChan:
			return nil
		case <-ticker.C:
			metrics, err := m.collectMetrics()
			if err != nil {
				continue
			}

			m.mutex.Lock()
			if len(m.metrics) >= m.maxSamples {
				m.metrics = m.metrics[len(m.metrics)/2:]
			}
			m.metrics = append(m.metrics, *metrics)
			m.mutex.Unlock()
		}
	}
}

func (m *Monitor) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.isRunning {
		close(m.stopChan)
		m.isRunning = false
	}
}

func (m *Monitor) GetMetrics() []SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	metrics := make([]SystemMetrics, len(m.metrics))
	copy(metrics, m.metrics)
	return metrics
}

func (m *Monitor) GetLatestMetrics() *SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if len(m.metrics) == 0 {
		return nil
	}

	latest := m.metrics[len(m.metrics)-1]
	return &latest
}

func (m *Monitor) GetAverageMetrics() *SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if len(m.metrics) == 0 {
		return nil
	}

	avg := &SystemMetrics{
		Timestamp:    time.Now(),
		MemoryUsage:  &MemoryMetrics{},
		DiskUsage:    &DiskMetrics{},
		NetworkUsage: &NetworkMetrics{},
		LoadAverage:  &LoadMetrics{},
	}

	var totalCPU []float64
	var totalMem, totalLoad float64
	count := float64(len(m.metrics))

	for _, metric := range m.metrics {
		if len(totalCPU) == 0 {
			totalCPU = make([]float64, len(metric.CPUPercent))
		}
		for i, cpu := range metric.CPUPercent {
			if i < len(totalCPU) {
				totalCPU[i] += cpu
			}
		}

		avg.MemoryUsage.Total += metric.MemoryUsage.Total
		avg.MemoryUsage.Used += metric.MemoryUsage.Used
		avg.MemoryUsage.Available += metric.MemoryUsage.Available
		avg.MemoryUsage.Free += metric.MemoryUsage.Free
		totalMem += metric.MemoryUsage.UsedPercent

		avg.DiskUsage.ReadBytes += metric.DiskUsage.ReadBytes
		avg.DiskUsage.WriteBytes += metric.DiskUsage.WriteBytes
		avg.DiskUsage.ReadCount += metric.DiskUsage.ReadCount
		avg.DiskUsage.WriteCount += metric.DiskUsage.WriteCount

		avg.NetworkUsage.BytesSent += metric.NetworkUsage.BytesSent
		avg.NetworkUsage.BytesRecv += metric.NetworkUsage.BytesRecv
		avg.NetworkUsage.PacketsSent += metric.NetworkUsage.PacketsSent
		avg.NetworkUsage.PacketsRecv += metric.NetworkUsage.PacketsRecv

		totalLoad += metric.LoadAverage.Load1
		avg.LoadAverage.Load5 += metric.LoadAverage.Load5
		avg.LoadAverage.Load15 += metric.LoadAverage.Load15
	}

	avg.CPUPercent = make([]float64, len(totalCPU))
	for i, total := range totalCPU {
		avg.CPUPercent[i] = total / count
	}

	avg.MemoryUsage.Total /= uint64(count)
	avg.MemoryUsage.Used /= uint64(count)
	avg.MemoryUsage.Available /= uint64(count)
	avg.MemoryUsage.Free /= uint64(count)
	avg.MemoryUsage.UsedPercent = totalMem / count

	avg.LoadAverage.Load1 = totalLoad / count
	avg.LoadAverage.Load5 /= count
	avg.LoadAverage.Load15 /= count

	return avg
}

func (m *Monitor) initializeBaselines() error {

	diskStats, err := disk.IOCounters()
	if err == nil && len(diskStats) > 0 {

		for _, stat := range diskStats {
			m.initialDisk = &stat
			break
		}
	}

	netStats, err := net.IOCounters(false)
	if err == nil && len(netStats) > 0 {
		m.initialNet = make(map[string]net.IOCountersStat)
		for _, stat := range netStats {
			m.initialNet[stat.Name] = stat
		}
	}

	return nil
}

func (m *Monitor) collectMetrics() (*SystemMetrics, error) {
	metrics := m.metricsPool.Get().(*SystemMetrics)
	metrics.Timestamp = time.Now()

	cpuPercents, err := cpu.Percent(0, true)
	if err == nil {
		if metrics.CPUPercent == nil || len(metrics.CPUPercent) != len(cpuPercents) {
			metrics.CPUPercent = make([]float64, len(cpuPercents))
		}
		copy(metrics.CPUPercent, cpuPercents)
	}

	memStats, err := mem.VirtualMemory()
	if err == nil {
		metrics.MemoryUsage.Total = memStats.Total
		metrics.MemoryUsage.Available = memStats.Available
		metrics.MemoryUsage.Used = memStats.Used
		metrics.MemoryUsage.UsedPercent = memStats.UsedPercent
		metrics.MemoryUsage.Free = memStats.Free
		metrics.MemoryUsage.Buffers = memStats.Buffers
		metrics.MemoryUsage.Cached = memStats.Cached
	}

	diskStats, err := disk.IOCounters()
	if err == nil && len(diskStats) > 0 {
		metrics.DiskUsage.ReadBytes = 0
		metrics.DiskUsage.WriteBytes = 0
		metrics.DiskUsage.ReadCount = 0
		metrics.DiskUsage.WriteCount = 0
		metrics.DiskUsage.ReadTime = 0
		metrics.DiskUsage.WriteTime = 0
		for _, stat := range diskStats {
			metrics.DiskUsage.ReadBytes += stat.ReadBytes
			metrics.DiskUsage.WriteBytes += stat.WriteBytes
			metrics.DiskUsage.ReadCount += stat.ReadCount
			metrics.DiskUsage.WriteCount += stat.WriteCount
			metrics.DiskUsage.ReadTime += stat.ReadTime
			metrics.DiskUsage.WriteTime += stat.WriteTime
		}
	}

	netStats, err := net.IOCounters(false)
	if err == nil && len(netStats) > 0 {
		metrics.NetworkUsage.BytesSent = 0
		metrics.NetworkUsage.BytesRecv = 0
		metrics.NetworkUsage.PacketsSent = 0
		metrics.NetworkUsage.PacketsRecv = 0
		metrics.NetworkUsage.ErrorsIn = 0
		metrics.NetworkUsage.ErrorsOut = 0
		metrics.NetworkUsage.DroppedIn = 0
		metrics.NetworkUsage.DroppedOut = 0
		for _, stat := range netStats {
			metrics.NetworkUsage.BytesSent += stat.BytesSent
			metrics.NetworkUsage.BytesRecv += stat.BytesRecv
			metrics.NetworkUsage.PacketsSent += stat.PacketsSent
			metrics.NetworkUsage.PacketsRecv += stat.PacketsRecv
			metrics.NetworkUsage.ErrorsIn += stat.Errin
			metrics.NetworkUsage.ErrorsOut += stat.Errout
			metrics.NetworkUsage.DroppedIn += stat.Dropin
			metrics.NetworkUsage.DroppedOut += stat.Dropout
		}
	}

	loadStats, err := load.Avg()
	if err == nil {
		metrics.LoadAverage.Load1 = loadStats.Load1
		metrics.LoadAverage.Load5 = loadStats.Load5
		metrics.LoadAverage.Load15 = loadStats.Load15
	}

	return metrics, nil
}

func (m *Monitor) GetPeakMetrics() *SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if len(m.metrics) == 0 {
		return nil
	}

	peak := &SystemMetrics{
		Timestamp:    time.Now(),
		MemoryUsage:  &MemoryMetrics{},
		DiskUsage:    &DiskMetrics{},
		NetworkUsage: &NetworkMetrics{},
		LoadAverage:  &LoadMetrics{},
	}

	var maxCPU []float64

	for _, metric := range m.metrics {
		if len(maxCPU) == 0 {
			maxCPU = make([]float64, len(metric.CPUPercent))
			copy(maxCPU, metric.CPUPercent)
		} else {
			for i, cpu := range metric.CPUPercent {
				if i < len(maxCPU) && cpu > maxCPU[i] {
					maxCPU[i] = cpu
				}
			}
		}

		if metric.MemoryUsage.Used > peak.MemoryUsage.Used {
			peak.MemoryUsage.Used = metric.MemoryUsage.Used
		}
		if metric.MemoryUsage.UsedPercent > peak.MemoryUsage.UsedPercent {
			peak.MemoryUsage.UsedPercent = metric.MemoryUsage.UsedPercent
		}

		if metric.LoadAverage.Load1 > peak.LoadAverage.Load1 {
			peak.LoadAverage.Load1 = metric.LoadAverage.Load1
		}
		if metric.LoadAverage.Load5 > peak.LoadAverage.Load5 {
			peak.LoadAverage.Load5 = metric.LoadAverage.Load5
		}
		if metric.LoadAverage.Load15 > peak.LoadAverage.Load15 {
			peak.LoadAverage.Load15 = metric.LoadAverage.Load15
		}
	}

	peak.CPUPercent = maxCPU
	return peak
}
