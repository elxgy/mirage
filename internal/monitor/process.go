package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

type ProcessMetrics struct {
	Timestamp       time.Time
	CPUPercent      float64
	MemoryRSS       uint64
	MemoryVMS       uint64
	ReadBytes       uint64
	WriteBytes      uint64
	ThreadCount     int32
	FileDescriptors int32
}

type ProcessMonitor struct {
	pid          int32
	interval     time.Duration
	metrics      []ProcessMetrics
	mutex        sync.RWMutex
	isRunning    bool
	stopChan     chan struct{}
	lastRead     uint64
	lastWrite    uint64
	hasBaselines bool
	OnSample     func(ProcessMetrics)
}

func NewProcessMonitor(pid int32, interval time.Duration) *ProcessMonitor {
	return &ProcessMonitor{
		pid:      pid,
		interval: interval,
		metrics:  make([]ProcessMetrics, 0, 128),
		stopChan: make(chan struct{}),
	}
}

func (pm *ProcessMonitor) Start(ctx context.Context) error {
	pm.mutex.Lock()
	if pm.isRunning {
		pm.mutex.Unlock()
		return nil
	}
	pm.isRunning = true
	pm.mutex.Unlock()

	ticker := time.NewTicker(pm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pm.stopChan:
			return nil
		case <-ticker.C:
			metrics := pm.collectMetrics()
			if metrics != nil {
				pm.mutex.Lock()
				pm.metrics = append(pm.metrics, *metrics)
				pm.mutex.Unlock()
				if pm.OnSample != nil {
					pm.OnSample(*metrics)
				}
			}
		}
	}
}

func (pm *ProcessMonitor) Stop() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.isRunning {
		close(pm.stopChan)
		pm.isRunning = false
	}
}

func (pm *ProcessMonitor) GetMetrics() []ProcessMetrics {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	metrics := make([]ProcessMetrics, len(pm.metrics))
	copy(metrics, pm.metrics)
	return metrics
}

func (pm *ProcessMonitor) collectMetrics() *ProcessMetrics {
	rootProc, err := process.NewProcess(pm.pid)
	if err != nil {
		return nil
	}

	// Get all children (including grandchildren)
	children, _ := rootProc.Children()
	procs := append([]*process.Process{rootProc}, children...)

	var totalCPU float64
	var totalRSS, totalVMS uint64
	var totalRead, totalWrite uint64
	var totalThreads, totalFDs int32

	for _, p := range procs {
		// CPU
		if cpu, err := p.Percent(0); err == nil {
			totalCPU += cpu
		}

		// Memory
		if mem, err := p.MemoryInfo(); err == nil {
			totalRSS += mem.RSS
			totalVMS += mem.VMS
		}

		// IO
		if io, err := p.IOCounters(); err == nil {
			totalRead += io.ReadBytes
			totalWrite += io.WriteBytes
		}

		// Threads & FDs
		if threads, err := p.NumThreads(); err == nil {
			totalThreads += threads
		}
		if fds, err := p.NumFDs(); err == nil {
			totalFDs += fds
		}
	}

	// Calculate IO deltas
	var readDelta, writeDelta uint64
	if pm.hasBaselines {
		if totalRead >= pm.lastRead {
			readDelta = totalRead - pm.lastRead
		}
		if totalWrite >= pm.lastWrite {
			writeDelta = totalWrite - pm.lastWrite
		}
	} else {
		pm.hasBaselines = true
	}
	pm.lastRead = totalRead
	pm.lastWrite = totalWrite

	return &ProcessMetrics{
		Timestamp:       time.Now(),
		CPUPercent:      totalCPU,
		MemoryRSS:       totalRSS,
		MemoryVMS:       totalVMS,
		ReadBytes:       readDelta,
		WriteBytes:      writeDelta,
		ThreadCount:     totalThreads,
		FileDescriptors: totalFDs,
	}
}
