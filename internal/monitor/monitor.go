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

type SystemMonitor struct {
	interval    time.Duration
	metrics     []SystemMetrics
	metricsPool *sync.Pool
	mutex       sync.RWMutex
	isRunning   bool
	stopChan    chan struct{}

	// Baselines for delta calculation
	lastDisk    map[string]disk.IOCountersStat
	lastNet     map[string]net.IOCountersStat
	hasBaseline bool

	maxSamples int
}

func NewSystemMonitor(interval time.Duration) *SystemMonitor {
	const defaultMaxSamples = 10000
	return &SystemMonitor{
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

func (m *SystemMonitor) Start(ctx context.Context) error {
	m.mutex.Lock()
	if m.isRunning {
		m.mutex.Unlock()
		return nil
	}
	m.isRunning = true
	m.mutex.Unlock()

	// Initialize baselines
	m.collectMetrics()

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

			// Only append if we have valid deltas (after first run)
			if metrics != nil {
				m.mutex.Lock()
				if len(m.metrics) >= m.maxSamples {
					m.metrics = m.metrics[len(m.metrics)/2:]
				}
				m.metrics = append(m.metrics, *metrics)
				m.mutex.Unlock()
			}
		}
	}
}

func (m *SystemMonitor) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.isRunning {
		close(m.stopChan)
		m.isRunning = false
	}
}

func (m *SystemMonitor) GetMetrics() []SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	metrics := make([]SystemMetrics, len(m.metrics))
	copy(metrics, m.metrics)
	return metrics
}

func (m *SystemMonitor) GetLatestMetrics() *SystemMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if len(m.metrics) == 0 {
		return nil
	}

	latest := m.metrics[len(m.metrics)-1]
	return &latest
}

func (m *SystemMonitor) collectMetrics() (*SystemMetrics, error) {
	metrics := m.metricsPool.Get().(*SystemMetrics)
	metrics.Timestamp = time.Now()

	// CPU
	cpuPercents, err := cpu.Percent(0, true)
	if err == nil {
		if metrics.CPUPercent == nil || len(metrics.CPUPercent) != len(cpuPercents) {
			metrics.CPUPercent = make([]float64, len(cpuPercents))
		}
		copy(metrics.CPUPercent, cpuPercents)
	}

	// Memory
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

	// Disk (Delta)
	diskStats, err := disk.IOCounters()
	if err == nil {
		metrics.DiskUsage.ReadBytes = 0
		metrics.DiskUsage.WriteBytes = 0
		metrics.DiskUsage.ReadCount = 0
		metrics.DiskUsage.WriteCount = 0

		currentDisk := make(map[string]disk.IOCountersStat)
		for name, stat := range diskStats {
			currentDisk[name] = stat
			if m.hasBaseline {
				if last, ok := m.lastDisk[name]; ok {
					if stat.ReadBytes >= last.ReadBytes {
						metrics.DiskUsage.ReadBytes += stat.ReadBytes - last.ReadBytes
					}
					if stat.WriteBytes >= last.WriteBytes {
						metrics.DiskUsage.WriteBytes += stat.WriteBytes - last.WriteBytes
					}
					if stat.ReadCount >= last.ReadCount {
						metrics.DiskUsage.ReadCount += stat.ReadCount - last.ReadCount
					}
					if stat.WriteCount >= last.WriteCount {
						metrics.DiskUsage.WriteCount += stat.WriteCount - last.WriteCount
					}
				}
			}
		}
		m.lastDisk = currentDisk
	}

	// Network (Delta)
	netStats, err := net.IOCounters(false)
	if err == nil {
		metrics.NetworkUsage.BytesSent = 0
		metrics.NetworkUsage.BytesRecv = 0
		metrics.NetworkUsage.PacketsSent = 0
		metrics.NetworkUsage.PacketsRecv = 0

		currentNet := make(map[string]net.IOCountersStat)
		for _, stat := range netStats {
			currentNet[stat.Name] = stat
			if m.hasBaseline {
				if last, ok := m.lastNet[stat.Name]; ok {
					if stat.BytesSent >= last.BytesSent {
						metrics.NetworkUsage.BytesSent += stat.BytesSent - last.BytesSent
					}
					if stat.BytesRecv >= last.BytesRecv {
						metrics.NetworkUsage.BytesRecv += stat.BytesRecv - last.BytesRecv
					}
					if stat.PacketsSent >= last.PacketsSent {
						metrics.NetworkUsage.PacketsSent += stat.PacketsSent - last.PacketsSent
					}
					if stat.PacketsRecv >= last.PacketsRecv {
						metrics.NetworkUsage.PacketsRecv += stat.PacketsRecv - last.PacketsRecv
					}
				}
			}
		}
		m.lastNet = currentNet
	}

	// Load Average
	loadStats, err := load.Avg()
	if err == nil {
		metrics.LoadAverage.Load1 = loadStats.Load1
		metrics.LoadAverage.Load5 = loadStats.Load5
		metrics.LoadAverage.Load15 = loadStats.Load15
	}

	if !m.hasBaseline {
		m.hasBaseline = true
		return nil, nil // Skip first sample as it's just baseline
	}

	return metrics, nil
}
