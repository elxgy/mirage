package profiler

import (
	"context"
	"fmt"
	"mirage/internal/cgroup"
	"mirage/internal/monitor"
	"mirage/internal/sampler"
	"mirage/internal/syscalltrace"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"syscall"
	"time"
)

type ProfileData struct {
	Command        string
	Args           []string
	StartTime      time.Time
	EndTime        time.Time
	Duration       time.Duration
	ExitCode       int
	CPUProfile     *CPUProfileData
	MemProfile     *MemoryProfileData
	ProcessStats   *ProcessStats
	SystemStats    *SystemStats
	ProcessMetrics []monitor.ProcessMetrics
	TracePath      string
	MutexPath       string
	CgroupStats     *cgroup.Stats
	SyscallSummary     []syscalltrace.SyscallStat
	TopSyscallsSampled []sampler.SyscallCount
	TargetPprofPath    string
	TargetPprofTop     []PprofTopEntry
}

type PprofTopEntry struct {
	Name  string
	Value int64
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
	PID        int32
	NumThreads int32
	NumFDs     int32
}

type SystemStats struct {
	CPUCount int
}

type Profiler struct {
	enableCPUProfile bool
	enableMemProfile bool
	enableTrace      bool
	enableMutex      bool
	enableCgroup     bool
	enableStrace     bool
	enableSample     bool
	profileDir       string
	monitorInterval  time.Duration
	metricsCallback  func(monitor.ProcessMetrics)
}

func NewWithTraceMutex(enableCPU, enableMem, enableTrace, enableMutex, enableCgroup, enableStrace, enableSample bool, profileDir string, monitorInterval time.Duration) *Profiler {
	return &Profiler{
		enableCPUProfile: enableCPU,
		enableMemProfile: enableMem,
		enableTrace:      enableTrace,
		enableMutex:      enableMutex,
		enableCgroup:     enableCgroup,
		enableStrace:     enableStrace,
		enableSample:     enableSample,
		profileDir:       profileDir,
		monitorInterval:  monitorInterval,
	}
}

func (p *Profiler) SetMetricsCallback(f func(monitor.ProcessMetrics)) {
	p.metricsCallback = f
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

	if p.enableTrace {
		tracePath := fmt.Sprintf("%s/trace.out", p.profileDir)
		traceFile, err := os.Create(tracePath)
		if err != nil {
			return data, fmt.Errorf("failed to create trace file: %v", err)
		}
		defer traceFile.Close()
		if err := trace.Start(traceFile); err != nil {
			return data, fmt.Errorf("failed to start trace: %v", err)
		}
		defer trace.Stop()
		data.TracePath = tracePath
	}

	data.SystemStats = p.getSystemStats()

	if err := cmd.Start(); err != nil {
		data.EndTime = time.Now()
		data.Duration = data.EndTime.Sub(data.StartTime)
		return data, fmt.Errorf("failed to start command: %v", err)
	}

	var cgScope *cgroup.Scope
	if p.enableCgroup {
		if scope, err := cgroup.NewScope(cmd.Process.Pid); err == nil {
			cgScope = scope
			defer func() { _ = cgScope.Cleanup() }()
		}
	}

	var straceSession *syscalltrace.Session
	if p.enableStrace {
		if sess, err := syscalltrace.StartAttach(cmd.Process.Pid); err == nil {
			straceSession = sess
		}
	}

	var samp *sampler.Sampler
	if p.enableSample {
		samp = sampler.New(cmd.Process.Pid, 10*time.Millisecond)
		sampCtx, sampCancel := context.WithCancel(ctx)
		go samp.Run(sampCtx)
		defer func() {
			sampCancel()
			samp.Stop()
		}()
	}

	procMon := monitor.NewProcessMonitor(int32(cmd.Process.Pid), p.monitorInterval)
	if p.metricsCallback != nil {
		procMon.OnSample = p.metricsCallback
	}
	procMonCtx, procMonCancel := context.WithCancel(ctx)

	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- procMon.Start(procMonCtx)
	}()

	err := cmd.Wait()

	// Stop Process Monitor
	procMonCancel()
	procMon.Stop()

	// Wait for monitor to finish
	select {
	case <-monitorDone:
	case <-time.After(500 * time.Millisecond):
	}

	data.EndTime = time.Now()
	data.Duration = data.EndTime.Sub(data.StartTime)
	data.ProcessMetrics = procMon.GetMetrics()

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

	if cgScope != nil {
		if st, err := cgScope.ReadStats(); err == nil {
			data.CgroupStats = &st
		}
	}

	if straceSession != nil {
		if summary, err := straceSession.Wait(); err == nil {
			data.SyscallSummary = summary
		}
	}

	if samp != nil {
		data.TopSyscallsSampled = sampler.TopSyscalls(samp.Samples(), 20)
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

	if p.enableMutex {
		mutexPath := fmt.Sprintf("%s/mutex.prof", p.profileDir)
		runtime.SetMutexProfileFraction(1)
		defer runtime.SetMutexProfileFraction(0)
		mutexFile, err := os.Create(mutexPath)
		if err != nil {
			return data, fmt.Errorf("failed to create mutex profile: %v", err)
		}
		defer mutexFile.Close()
		if err := pprof.Lookup("mutex").WriteTo(mutexFile, 0); err != nil {
			return data, fmt.Errorf("failed to write mutex profile: %v", err)
		}
		data.MutexPath = mutexPath
	}

	return data, nil
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

	// Fill ProcessStats from the last metric if available
	if len(data.ProcessMetrics) > 0 {
		lastMetric := data.ProcessMetrics[len(data.ProcessMetrics)-1]
		data.ProcessStats = &ProcessStats{
			PID:        int32(cmd.Process.Pid),
			NumThreads: lastMetric.ThreadCount,
			NumFDs:     lastMetric.FileDescriptors,
		}
	}

	return nil
}

func (p *Profiler) getSystemStats() *SystemStats {
	stats := &SystemStats{
		CPUCount: runtime.NumCPU(),
	}

	return stats
}
