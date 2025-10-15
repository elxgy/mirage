package profiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

type ProfileData struct {
	Command      string
	Args         []string
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	ExitCode     int
	CPUProfile   *CPUProfileData
	MemProfile   *MemoryProfileData
	ProcessStats *ProcessStats
	SystemStats  *SystemStats
	StackTraces  []string
}

type CPUProfileData struct {
	UserTime   time.Duration
	SystemTime time.Duration
	TotalTime  time.Duration
	CPUPercent float64
}

type MemoryProfileData struct {
	PeakRSS       uint64
	PeakVMS       uint64
	MinorFaults   uint64
	MajorFaults   uint64
	InitialMemory uint64
	PeakMemory    uint64
}

type ProcessStats struct {
	PID             int32
	PPID            int32
	NumThreads      int32
	NumFDs          int32
	ContextSwitches *process.NumCtxSwitchesStat
}

type SystemStats struct {
	CPUCount    int
	LoadAvg     []float64
	MemoryTotal uint64
	MemoryFree  uint64
}

type Profiler struct {
	enableCPUProfile bool
	enableMemProfile bool
	profileDir       string
	monitorInterval  time.Duration
}

func New(enableCPU, enableMem bool, profileDir string) *Profiler {
	return &Profiler{
		enableCPUProfile: enableCPU,
		enableMemProfile: enableMem,
		profileDir:       profileDir,
		monitorInterval:  50 * time.Millisecond,
	}
}

func (p *Profiler) Profile(ctx context.Context, command string, args ...string) (*ProfileData, error) {
	data := &ProfileData{
		Command:   command,
		Args:      args,
		StartTime: time.Now(),
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	var cpuProfileFile *os.File
	if p.enableCPUProfile {
		var err error
		cpuProfileFile, err = os.Create(fmt.Sprintf("%s/cpu.prof", p.profileDir))
		if err != nil {
			return data, fmt.Errorf("failed to create CPU profile: %v", err)
		}
		defer cpuProfileFile.Close()

		if err := pprof.StartCPUProfile(cpuProfileFile); err != nil {
			return data, fmt.Errorf("failed to start CPU profile: %v", err)
		}
		defer pprof.StopCPUProfile()
	}

	data.SystemStats = p.getSystemStats()

	if err := cmd.Start(); err != nil {
		data.EndTime = time.Now()
		data.Duration = data.EndTime.Sub(data.StartTime)
		return data, fmt.Errorf("failed to start command: %v", err)
	}

	proc, err := process.NewProcess(int32(cmd.Process.Pid))
	if err != nil {
		return data, fmt.Errorf("failed to get process info: %v", err)
	}

	monitorDone := make(chan struct{})
	go p.monitorProcess(proc, data, monitorDone)

	err = cmd.Wait()
	close(monitorDone)

	data.EndTime = time.Now()
	data.Duration = data.EndTime.Sub(data.StartTime)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			data.ExitCode = exitError.ExitCode()
		} else {
			data.ExitCode = -1
		}
	} else {
		data.ExitCode = 0
	}

	if err := p.getFinalStats(cmd, data); err != nil {
		return data, fmt.Errorf("failed to get final stats: %v", err)
	}

	if p.enableMemProfile {
		memProfileFile, err := os.Create(fmt.Sprintf("%s/mem.prof", p.profileDir))
		if err != nil {
			return data, fmt.Errorf("failed to create memory profile: %v", err)
		}
		defer memProfileFile.Close()

		runtime.GC()
		if err := pprof.WriteHeapProfile(memProfileFile); err != nil {
			return data, fmt.Errorf("failed to write memory profile: %v", err)
		}

		goroutineProfile, err := os.Create(fmt.Sprintf("%s/goroutine.prof", p.profileDir))
		if err == nil {
			defer goroutineProfile.Close()
			pprof.Lookup("goroutine").WriteTo(goroutineProfile, 0)
		}

		blockProfile, err := os.Create(fmt.Sprintf("%s/block.prof", p.profileDir))
		if err == nil {
			defer blockProfile.Close()
			runtime.SetBlockProfileRate(1)
			pprof.Lookup("block").WriteTo(blockProfile, 0)
		}
	}

	return data, nil
}

func (p *Profiler) monitorProcess(proc *process.Process, data *ProfileData, done <-chan struct{}) {
	ticker := time.NewTicker(p.monitorInterval)
	defer ticker.Stop()

	var maxRSS, maxVMS uint64
	var sampleCount int

	if data.MemProfile == nil {
		data.MemProfile = &MemoryProfileData{}
	}
	if data.ProcessStats == nil {
		data.ProcessStats = &ProcessStats{PID: proc.Pid}
	}

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			sampleCount++

			memInfo, err := proc.MemoryInfo()
			if err != nil {
				continue
			}

			if memInfo.RSS > maxRSS {
				maxRSS = memInfo.RSS
				data.MemProfile.PeakRSS = maxRSS
			}
			if memInfo.VMS > maxVMS {
				maxVMS = memInfo.VMS
				data.MemProfile.PeakVMS = maxVMS
			}

			if sampleCount%10 == 0 {
				if numThreads, err := proc.NumThreads(); err == nil {
					data.ProcessStats.NumThreads = numThreads
				}

				if numFDs, err := proc.NumFDs(); err == nil {
					data.ProcessStats.NumFDs = numFDs
				}

				if ctxSwitches, err := proc.NumCtxSwitches(); err == nil {
					data.ProcessStats.ContextSwitches = ctxSwitches
				}
			}
		}
	}
}

func (p *Profiler) getFinalStats(cmd *exec.Cmd, data *ProfileData) error {
	if cmd.ProcessState == nil {
		return nil
	}

	if data.CPUProfile == nil {
		data.CPUProfile = &CPUProfileData{}
	}

	sysUsage := cmd.ProcessState.SysUsage()
	if rusage, ok := sysUsage.(*syscall.Rusage); ok {
		data.CPUProfile.UserTime = time.Duration(rusage.Utime.Sec)*time.Second + time.Duration(rusage.Utime.Usec)*time.Microsecond
		data.CPUProfile.SystemTime = time.Duration(rusage.Stime.Sec)*time.Second + time.Duration(rusage.Stime.Usec)*time.Microsecond
		data.CPUProfile.TotalTime = data.CPUProfile.UserTime + data.CPUProfile.SystemTime

		if data.Duration > 0 {
			data.CPUProfile.CPUPercent = float64(data.CPUProfile.TotalTime) / float64(data.Duration) * 100
		}

		if data.MemProfile == nil {
			data.MemProfile = &MemoryProfileData{}
		}
		data.MemProfile.PeakRSS = uint64(rusage.Maxrss) * 1024
		data.MemProfile.MinorFaults = uint64(rusage.Minflt)
		data.MemProfile.MajorFaults = uint64(rusage.Majflt)
	}

	return nil
}

func (p *Profiler) getSystemStats() *SystemStats {
	stats := &SystemStats{
		CPUCount: runtime.NumCPU(),
	}

	return stats
}
