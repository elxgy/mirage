package report

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"

	"mirage/internal/analysis"
	"mirage/internal/monitor"
	"mirage/internal/profiler"
)

type ReportConfig struct {
	OutputFile  string
	Format      ReportFormat
	Verbose     bool
	ColorOutput bool
}

type ReportFormat int

const (
	FormatText ReportFormat = iota
	FormatMarkdown
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

func (r *Reporter) GenerateReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics, findings []analysis.Finding, tracePath, mutexPath string) error {
	switch r.config.Format {
	case FormatMarkdown:
		return r.generateMarkdownReport(profileData, systemMetrics, findings, tracePath, mutexPath)
	default:
		return r.generateTextReport(profileData, systemMetrics, findings, tracePath, mutexPath)
	}
}

func (r *Reporter) generateTextReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics, findings []analysis.Finding, tracePath, mutexPath string) error {
	var (
		titleColor   = color.New(color.FgCyan, color.Bold)
		successColor = color.New(color.FgGreen)
		errorColor   = color.New(color.FgRed)
		labelColor   = color.New(color.FgHiWhite)
		valueColor   = color.New(color.FgWhite)
	)

	if !r.config.ColorOutput {
		color.NoColor = true
	}

	// Title Box
	r.drawBox("MIRAGE PERFORMANCE REPORT", func(w io.Writer) {
		titleColor.Fprintln(w, "  Target Command Analysis")
	})
	fmt.Fprintln(r.writer)

	// Command Info
	r.drawBox("Command Information", func(w io.Writer) {
		r.writeKV(w, "Command", fmt.Sprintf("%s %s", profileData.Command, strings.Join(profileData.Args, " ")), labelColor, valueColor)
		r.writeKV(w, "Start Time", profileData.StartTime.Format(time.RFC3339), labelColor, valueColor)
		r.writeKV(w, "End Time", profileData.EndTime.Format(time.RFC3339), labelColor, valueColor)
		r.writeKV(w, "Duration", profileData.Duration.String(), labelColor, valueColor)

		exitStatus := fmt.Sprintf("%d (Success)", profileData.ExitCode)
		exitColor := successColor
		if profileData.ExitCode != 0 {
			exitStatus = fmt.Sprintf("%d (Error)", profileData.ExitCode)
			exitColor = errorColor
		}
		r.writeKV(w, "Exit Code", exitStatus, labelColor, exitColor)
	})
	fmt.Fprintln(r.writer)

	// Process Metrics
	if len(profileData.ProcessMetrics) > 0 {
		r.drawBox("Process Metrics (Target + Children)", func(w io.Writer) {
			// Calculate averages/peaks
			var peakCPU float64
			var peakRSS, peakVMS uint64
			var totalRead, totalWrite uint64

			for _, m := range profileData.ProcessMetrics {
				if m.CPUPercent > peakCPU {
					peakCPU = m.CPUPercent
				}
				if m.MemoryRSS > peakRSS {
					peakRSS = m.MemoryRSS
				}
				if m.MemoryVMS > peakVMS {
					peakVMS = m.MemoryVMS
				}
				totalRead += m.ReadBytes
				totalWrite += m.WriteBytes
			}

			// Get last stats for threads/fds
			last := profileData.ProcessMetrics[len(profileData.ProcessMetrics)-1]

			r.writeKV(w, "Peak CPU Usage", fmt.Sprintf("%.2f%%", peakCPU), labelColor, valueColor)
			r.writeKV(w, "Peak RSS Memory", r.formatBytes(peakRSS), labelColor, valueColor)
			r.writeKV(w, "Peak VMS Memory", r.formatBytes(peakVMS), labelColor, valueColor)
			r.writeKV(w, "Total Read", r.formatBytes(totalRead), labelColor, valueColor)
			r.writeKV(w, "Total Write", r.formatBytes(totalWrite), labelColor, valueColor)
			r.writeKV(w, "Active Threads", fmt.Sprintf("%d", last.ThreadCount), labelColor, valueColor)
			r.writeKV(w, "Open Files", fmt.Sprintf("%d", last.FileDescriptors), labelColor, valueColor)
		})
		fmt.Fprintln(r.writer)
	}

	// System Metrics
	if len(systemMetrics) > 0 {
		r.drawBox("System Resource Usage (Total)", func(w io.Writer) {
			r.writeSystemMetricsReport(w, systemMetrics)
		})
		fmt.Fprintln(r.writer)
	}

	r.drawBox("Findings", func(w io.Writer) {
		r.writeFindings(w, findings)
	})
	fmt.Fprintln(r.writer)

	if tracePath != "" || mutexPath != "" {
		r.drawBox("Profile Files", func(w io.Writer) {
			if tracePath != "" {
				r.writeKV(w, "Execution trace", tracePath+" (go tool trace)", labelColor, valueColor)
			}
			if mutexPath != "" {
				r.writeKV(w, "Mutex profile", mutexPath+" (go tool pprof)", labelColor, valueColor)
			}
		})
		fmt.Fprintln(r.writer)
	}

	return nil
}

func (r *Reporter) drawBox(title string, contentFunc func(io.Writer)) {
	borderColor := color.New(color.FgHiBlack)
	titleColor := color.New(color.FgCyan, color.Bold)

	width := 80

	// Top border
	borderColor.Fprint(r.writer, "╭─ ")
	titleColor.Fprint(r.writer, title)
	borderColor.Fprintln(r.writer, " "+strings.Repeat("─", width-len(title)-4)+"╮")

	// Content buffer to handle indentation/padding
	var buf strings.Builder
	contentFunc(&buf)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for _, line := range lines {
		// Simple padding, could be more sophisticated
		visibleLength := utf8.RuneCountInString(stripAnsi(line))
		padding := width - visibleLength - 2
		if padding < 0 {
			padding = 0
		}

		borderColor.Fprint(r.writer, "│ ")
		fmt.Fprint(r.writer, line)
		fmt.Fprint(r.writer, strings.Repeat(" ", padding))
		borderColor.Fprintln(r.writer, "│")
	}

	// Bottom border
	borderColor.Fprintln(r.writer, "╰"+strings.Repeat("─", width-1)+"╯")
}

func (r *Reporter) writeKV(w io.Writer, key, value string, keyColor, valColor *color.Color) {
	keyColor.Fprintf(w, "%-25s", key+":")
	valColor.Fprintln(w, value)
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

func (r *Reporter) writeSystemMetricsReport(w io.Writer, metrics []monitor.SystemMetrics) {
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

	// Using a simple list format inside the box instead of a complex table for cleaner look
	labelColor := color.New(color.FgHiWhite)
	valueColor := color.New(color.FgWhite)

	r.writeKV(w, "Avg System CPU", fmt.Sprintf("%.2f%% (Peak: %.2f%%)", avgCPU, peakCPU), labelColor, valueColor)
	r.writeKV(w, "Avg System Memory", fmt.Sprintf("%.2f%% (Peak: %.2f%%)", avgMemPercent, peakMemPercent), labelColor, valueColor)
	r.writeKV(w, "Peak Memory Used", r.formatBytes(peakMemUsed), labelColor, valueColor)
	r.writeKV(w, "Avg Load (1m)", fmt.Sprintf("%.2f (Peak: %.2f)", avgLoad1, peakLoad1), labelColor, valueColor)
}

func (r *Reporter) writeFindings(w io.Writer, findings []analysis.Finding) {
	infoColor := color.New(color.FgCyan)
	warningColor := color.New(color.FgYellow)
	criticalColor := color.New(color.FgRed)
	okColor := color.New(color.FgGreen)

	if len(findings) == 0 {
		okColor.Fprintln(w, "• No significant performance anomalies detected.")
		return
	}
	for _, f := range findings {
		var sev *color.Color
		switch f.Severity {
		case analysis.SeverityCritical:
			sev = criticalColor
		case analysis.SeverityWarning:
			sev = warningColor
		default:
			sev = infoColor
		}
		sev.Fprintf(w, "• [%s] %s\n", f.Severity, f.Message)
		fmt.Fprintf(w, "  Evidence: %s\n", f.Evidence)
		fmt.Fprintf(w, "  Recommendation: %s\n", f.Recommendation)
	}
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

func (r *Reporter) Close() error {
	if r.bufferedFile != nil && r.bufferedFile != os.Stdout && r.bufferedFile != os.Stderr {
		return r.bufferedFile.Close()
	}
	return nil
}

func (r *Reporter) generateMarkdownReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics, findings []analysis.Finding, tracePath, mutexPath string) error {
	w := r.writer
	fmt.Fprintf(w, "# Mirage Performance Report\n\n")
	fmt.Fprintf(w, "## Command Information\n\n")
	fmt.Fprintf(w, "- **Command:** %s %s\n", profileData.Command, strings.Join(profileData.Args, " "))
	fmt.Fprintf(w, "- **Start Time:** %s\n", profileData.StartTime.Format(time.RFC3339))
	fmt.Fprintf(w, "- **End Time:** %s\n", profileData.EndTime.Format(time.RFC3339))
	fmt.Fprintf(w, "- **Duration:** %s\n", profileData.Duration.String())
	exitLabel := "Success"
	if profileData.ExitCode != 0 {
		exitLabel = "Error"
	}
	fmt.Fprintf(w, "- **Exit Code:** %d (%s)\n\n", profileData.ExitCode, exitLabel)

	if len(profileData.ProcessMetrics) > 0 {
		var peakCPU float64
		var peakRSS, peakVMS uint64
		var totalRead, totalWrite uint64
		for _, m := range profileData.ProcessMetrics {
			if m.CPUPercent > peakCPU {
				peakCPU = m.CPUPercent
			}
			if m.MemoryRSS > peakRSS {
				peakRSS = m.MemoryRSS
			}
			if m.MemoryVMS > peakVMS {
				peakVMS = m.MemoryVMS
			}
			totalRead += m.ReadBytes
			totalWrite += m.WriteBytes
		}
		last := profileData.ProcessMetrics[len(profileData.ProcessMetrics)-1]
		fmt.Fprintf(w, "## Process Metrics (Target + Children)\n\n")
		fmt.Fprintf(w, "- **Peak CPU Usage:** %.2f%%\n", peakCPU)
		fmt.Fprintf(w, "- **Peak RSS Memory:** %s\n", r.formatBytes(peakRSS))
		fmt.Fprintf(w, "- **Peak VMS Memory:** %s\n", r.formatBytes(peakVMS))
		fmt.Fprintf(w, "- **Total Read:** %s\n", r.formatBytes(totalRead))
		fmt.Fprintf(w, "- **Total Write:** %s\n", r.formatBytes(totalWrite))
		fmt.Fprintf(w, "- **Active Threads:** %d\n", last.ThreadCount)
		fmt.Fprintf(w, "- **Open Files:** %d\n\n", last.FileDescriptors)
	}

	if len(systemMetrics) > 0 {
		var avgCPU, peakCPU, avgMemPercent, peakMemPercent float64
		var peakMemUsed uint64
		var avgLoad1, peakLoad1 float64
		metricsLen := len(systemMetrics)
		sampleRate := 1
		if metricsLen > 1000 {
			sampleRate = metricsLen / 1000
		}
		sampledCount := 0
		for i := 0; i < metricsLen; i += sampleRate {
			m := systemMetrics[i]
			sampledCount++
			if len(m.CPUPercent) > 0 {
				var totalCPU float64
				for _, cpu := range m.CPUPercent {
					totalCPU += cpu
					if cpu > peakCPU {
						peakCPU = cpu
					}
				}
				avgCPU += totalCPU / float64(len(m.CPUPercent))
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
		if count > 0 {
			avgCPU /= count
			avgMemPercent /= count
			avgLoad1 /= count
		}
		fmt.Fprintf(w, "## System Resource Usage\n\n")
		fmt.Fprintf(w, "- **Avg System CPU:** %.2f%% (Peak: %.2f%%)\n", avgCPU, peakCPU)
		fmt.Fprintf(w, "- **Avg System Memory:** %.2f%% (Peak: %.2f%%)\n", avgMemPercent, peakMemPercent)
		fmt.Fprintf(w, "- **Peak Memory Used:** %s\n", r.formatBytes(peakMemUsed))
		fmt.Fprintf(w, "- **Avg Load (1m):** %.2f (Peak: %.2f)\n\n", avgLoad1, peakLoad1)
	}

	fmt.Fprintf(w, "## Findings\n\n")
	if len(findings) == 0 {
		fmt.Fprintf(w, "No significant performance anomalies detected.\n\n")
	} else {
		for _, f := range findings {
			fmt.Fprintf(w, "- **[%s]** %s\n", f.Severity, f.Message)
			fmt.Fprintf(w, "  - Evidence: %s\n", f.Evidence)
			fmt.Fprintf(w, "  - Recommendation: %s\n\n", f.Recommendation)
		}
	}

	if tracePath != "" || mutexPath != "" {
		fmt.Fprintf(w, "## Profile Files\n\n")
		if tracePath != "" {
			fmt.Fprintf(w, "- **Execution trace:** %s (view with `go tool trace`)\n", tracePath)
		}
		if mutexPath != "" {
			fmt.Fprintf(w, "- **Mutex profile:** %s (view with `go tool pprof`)\n", mutexPath)
		}
	}

	return nil
}
