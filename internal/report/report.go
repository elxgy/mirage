package report

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"

	"mirage/internal/monitor"
	"mirage/internal/profiler"
)

type ReportConfig struct {
	OutputFile   string
	Format       ReportFormat
	Verbose      bool
	ShowGraphs   bool
	ColorOutput  bool
	IncludePprof bool
}

type ReportFormat int

const (
	FormatText ReportFormat = iota
	FormatJSON
	FormatHTML
	FormatCSV
)

type Reporter struct {
	config       ReportConfig
	writer       io.Writer
	bufferedFile *os.File
}

func New(config ReportConfig) *Reporter {
	reporter := &Reporter{
		config: config,
		writer: os.Stdout,
	}

	if config.OutputFile != "" {
		if file, err := os.Create(config.OutputFile); err == nil {
			reporter.bufferedFile = file
			reporter.writer = file
		}
	}

	return reporter
}

func (r *Reporter) GenerateReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	switch r.config.Format {
	case FormatText:
		return r.generateTextReport(profileData, systemMetrics)
	case FormatJSON:
		return r.generateJSONReport(profileData, systemMetrics)
	case FormatHTML:
		return r.generateHTMLReport(profileData, systemMetrics)
	case FormatCSV:
		return r.generateCSVReport(profileData, systemMetrics)
	default:
		return r.generateTextReport(profileData, systemMetrics)
	}
}

func (r *Reporter) generateTextReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	var (
		successColor = color.New(color.FgGreen)
		errorColor   = color.New(color.FgRed)
	)

	if !r.config.ColorOutput {
		color.NoColor = true
	}

	r.writeSection("MIRAGE PERFORMANCE REPORT", "=")

	r.writeSection("Command Information", "-")
	fmt.Fprintf(r.writer, "Command: %s %s\n", profileData.Command, strings.Join(profileData.Args, " "))
	fmt.Fprintf(r.writer, "Start Time: %s\n", profileData.StartTime.Format(time.RFC3339))
	fmt.Fprintf(r.writer, "End Time: %s\n", profileData.EndTime.Format(time.RFC3339))
	fmt.Fprintf(r.writer, "Duration: %s\n", profileData.Duration)

	if profileData.ExitCode == 0 {
		successColor.Fprintf(r.writer, "Exit Code: %d (Success)\n", profileData.ExitCode)
	} else {
		errorColor.Fprintf(r.writer, "Exit Code: %d (Error)\n", profileData.ExitCode)
	}
	fmt.Fprintln(r.writer)

	if profileData.CPUProfile != nil {
		r.writeSection("CPU Performance", "-")
		r.writeCPUReport(profileData.CPUProfile)
	}

	if profileData.MemProfile != nil {
		r.writeSection("Memory Usage", "-")
		r.writeMemoryReport(profileData.MemProfile)
	}

	if profileData.ProcessStats != nil {
		r.writeSection("Process Statistics", "-")
		r.writeProcessReport(profileData.ProcessStats)
	}

	if len(systemMetrics) > 0 {
		r.writeSection("System Resource Usage", "-")
		r.writeSystemMetricsReport(systemMetrics)
	}

	r.writeSection("Performance Analysis", "-")
	r.writePerformanceAnalysis(profileData, systemMetrics)

	r.writeSection("Optimization Recommendations", "-")
	r.writeRecommendations(profileData, systemMetrics)

	return nil
}

func (r *Reporter) writeSection(title, separator string) {
	fmt.Fprintln(r.writer)
	fmt.Fprintf(r.writer, "%s %s %s\n", strings.Repeat(separator, 10), title, strings.Repeat(separator, 10))
	fmt.Fprintln(r.writer)
}

func (r *Reporter) writeCPUReport(cpu *profiler.CPUProfileData) {
	r.writeTable([][]string{
		{"Metric", "Value"},
		{"User Time", cpu.UserTime.String()},
		{"System Time", cpu.SystemTime.String()},
		{"Total CPU Time", cpu.TotalTime.String()},
		{"CPU Utilization", fmt.Sprintf("%.2f%%", cpu.CPUPercent)},
	})
	fmt.Fprintln(r.writer)
}

func (r *Reporter) writeMemoryReport(mem *profiler.MemoryProfileData) {
	r.writeTable([][]string{
		{"Metric", "Value"},
		{"Peak RSS", r.formatBytes(mem.PeakRSS)},
		{"Peak VMS", r.formatBytes(mem.PeakVMS)},
		{"Minor Page Faults", fmt.Sprintf("%d", mem.MinorFaults)},
		{"Major Page Faults", fmt.Sprintf("%d", mem.MajorFaults)},
	})
	fmt.Fprintln(r.writer)
}

func (r *Reporter) writeProcessReport(proc *profiler.ProcessStats) {
	data := [][]string{
		{"Metric", "Value"},
		{"Process ID", fmt.Sprintf("%d", proc.PID)},
		{"Parent PID", fmt.Sprintf("%d", proc.PPID)},
		{"Threads", fmt.Sprintf("%d", proc.NumThreads)},
		{"File Descriptors", fmt.Sprintf("%d", proc.NumFDs)},
	}

	if proc.ContextSwitches != nil {
		data = append(data, []string{"Voluntary Context Switches", fmt.Sprintf("%d", proc.ContextSwitches.Voluntary)})
		data = append(data, []string{"Involuntary Context Switches", fmt.Sprintf("%d", proc.ContextSwitches.Involuntary)})
	}

	r.writeTable(data)
	fmt.Fprintln(r.writer)
}

func (r *Reporter) writeSystemMetricsReport(metrics []monitor.SystemMetrics) {
	if len(metrics) == 0 {
		return
	}

	var avgCPU, peakCPU, avgMemPercent, peakMemPercent float64
	var peakMemUsed uint64
	var avgLoad1, peakLoad1 float64

	metricsLen := len(metrics)
	sampleRate := 1
	if metricsLen > 1000 {
		sampleRate = metricsLen / 1000
	}

	sampledCount := 0
	for i := 0; i < metricsLen; i += sampleRate {
		m := metrics[i]
		sampledCount++

		if len(m.CPUPercent) > 0 {
			var totalCPU float64
			for _, cpu := range m.CPUPercent {
				totalCPU += cpu
				if cpu > peakCPU {
					peakCPU = cpu
				}
			}
			cpuAvg := totalCPU / float64(len(m.CPUPercent))
			avgCPU += cpuAvg
		}

		if m.MemoryUsage != nil {
			avgMemPercent += m.MemoryUsage.UsedPercent
			if m.MemoryUsage.UsedPercent > peakMemPercent {
				peakMemPercent = m.MemoryUsage.UsedPercent
			}
			if m.MemoryUsage.Used > peakMemUsed {
				peakMemUsed = m.MemoryUsage.Used
			}
		}

		if m.LoadAverage != nil {
			avgLoad1 += m.LoadAverage.Load1
			if m.LoadAverage.Load1 > peakLoad1 {
				peakLoad1 = m.LoadAverage.Load1
			}
		}
	}

	count := float64(sampledCount)
	avgCPU /= count
	avgMemPercent /= count
	avgLoad1 /= count

	r.writeTable([][]string{
		{"Resource", "Average", "Peak"},
		{"CPU Usage", fmt.Sprintf("%.2f%%", avgCPU), fmt.Sprintf("%.2f%%", peakCPU)},
		{"Memory Usage", fmt.Sprintf("%.2f%%", avgMemPercent), fmt.Sprintf("%.2f%%", peakMemPercent)},
		{"Memory Used", r.formatBytes(uint64(avgMemPercent / 100 * float64(peakMemUsed))), r.formatBytes(peakMemUsed)},
		{"Load Average (1m)", fmt.Sprintf("%.2f", avgLoad1), fmt.Sprintf("%.2f", peakLoad1)},
	})
	fmt.Fprintln(r.writer)
}

func (r *Reporter) writePerformanceAnalysis(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) {
	warningColor := color.New(color.FgYellow)
	infoColor := color.New(color.FgCyan)

	if profileData.CPUProfile != nil {
		if profileData.CPUProfile.CPUPercent > 80 {
			warningColor.Fprintf(r.writer, "High CPU utilization detected (%.2f%%)\n", profileData.CPUProfile.CPUPercent)
		} else if profileData.CPUProfile.CPUPercent < 10 {
			infoColor.Fprintf(r.writer, "Low CPU utilization (%.2f%%) - may indicate I/O bound process\n", profileData.CPUProfile.CPUPercent)
		}

		userTimePercent := float64(profileData.CPUProfile.UserTime) / float64(profileData.CPUProfile.TotalTime) * 100
		if userTimePercent > 80 {
			infoColor.Fprintf(r.writer, "CPU time mostly spent in user space (%.2f%%)\n", userTimePercent)
		} else if userTimePercent < 20 {
			warningColor.Fprintf(r.writer, "High system time usage (%.2f%%) - potential system call overhead\n", 100-userTimePercent)
		}
	}

	if profileData.MemProfile != nil {
		if profileData.MemProfile.MajorFaults > 1000 {
			warningColor.Fprintf(r.writer, "High major page faults (%d) - potential memory pressure\n", profileData.MemProfile.MajorFaults)
		}

		if profileData.MemProfile.PeakRSS > 1024*1024*1024 {
			infoColor.Fprintf(r.writer, "High memory usage detected: %s\n", r.formatBytes(profileData.MemProfile.PeakRSS))
		}
	}

	if profileData.Duration > 10*time.Second {
		infoColor.Fprintf(r.writer, "Long running process detected (%s)\n", profileData.Duration)
	}

	fmt.Fprintln(r.writer)
}

func (r *Reporter) writeRecommendations(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) {
	recommendations := []string{}

	if profileData.CPUProfile != nil {
		if profileData.CPUProfile.CPUPercent > 80 {
			recommendations = append(recommendations, "Consider optimizing CPU-intensive code paths or using parallel processing")
		}

		userTimePercent := float64(profileData.CPUProfile.UserTime) / float64(profileData.CPUProfile.TotalTime) * 100
		if userTimePercent < 20 {
			recommendations = append(recommendations, "High system time suggests frequent system calls - consider batching operations")
		}
	}

	if profileData.MemProfile != nil {
		if profileData.MemProfile.MajorFaults > 1000 {
			recommendations = append(recommendations, "High page faults suggest memory pressure - consider increasing available memory or optimizing memory usage")
		}

		if profileData.MemProfile.PeakRSS > 1024*1024*1024 {
			recommendations = append(recommendations, "High memory usage detected - consider memory profiling to identify memory leaks or inefficient allocations")
		}
	}

	if profileData.ProcessStats != nil {
		if profileData.ProcessStats.ContextSwitches != nil {
			totalSwitches := profileData.ProcessStats.ContextSwitches.Voluntary + profileData.ProcessStats.ContextSwitches.Involuntary
			if totalSwitches > 10000 {
				recommendations = append(recommendations, "High context switch count suggests potential threading issues or resource contention")
			}
		}

		if profileData.ProcessStats.NumFDs > 100 {
			recommendations = append(recommendations, "High file descriptor usage - ensure proper resource cleanup and consider connection pooling")
		}
	}

	if profileData.Duration > 30*time.Second {
		recommendations = append(recommendations, "Long execution time - consider profiling with pprof for detailed analysis")
	}

	if len(recommendations) == 0 {
		fmt.Fprintf(r.writer, "No significant performance issues detected\n")
	} else {
		for i, rec := range recommendations {
			fmt.Fprintf(r.writer, "%d. %s\n", i+1, rec)
		}
	}

	fmt.Fprintln(r.writer)
}

func (r *Reporter) formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (r *Reporter) writeTable(data [][]string) {
	if len(data) == 0 {
		return
	}

	colWidths := make([]int, len(data[0]))
	for _, row := range data {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	for i := range colWidths {
		colWidths[i] += 2
	}

	fmt.Fprint(r.writer, "+")
	for _, width := range colWidths {
		fmt.Fprint(r.writer, strings.Repeat("-", width)+"+")
	}
	fmt.Fprintln(r.writer)

	for i, row := range data {
		fmt.Fprint(r.writer, "|")
		for j, cell := range row {
			if j < len(colWidths) {
				fmt.Fprintf(r.writer, " %-*s |", colWidths[j]-2, cell)
			}
		}
		fmt.Fprintln(r.writer)

		if i == 0 {
			fmt.Fprint(r.writer, "+")
			for _, width := range colWidths {
				fmt.Fprint(r.writer, strings.Repeat("-", width)+"+")
			}
			fmt.Fprintln(r.writer)
		}
	}

	fmt.Fprint(r.writer, "+")
	for _, width := range colWidths {
		fmt.Fprint(r.writer, strings.Repeat("-", width)+"+")
	}
	fmt.Fprintln(r.writer)
}

func (r *Reporter) Close() error {
	if r.bufferedFile != nil && r.bufferedFile != os.Stdout && r.bufferedFile != os.Stderr {
		return r.bufferedFile.Close()
	}
	return nil
}

func (r *Reporter) generateJSONReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	return fmt.Errorf("JSON format not yet implemented")
}

func (r *Reporter) generateHTMLReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	return fmt.Errorf("HTML format not yet implemented")
}

func (r *Reporter) generateCSVReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	return fmt.Errorf("CSV format not yet implemented")
}
