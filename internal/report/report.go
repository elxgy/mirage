package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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

	// Analysis & Recommendations
	r.drawBox("Analysis & Recommendations", func(w io.Writer) {
		r.writePerformanceAnalysis(w, profileData, systemMetrics)
		fmt.Fprintln(w)
		r.writeRecommendations(w, profileData, systemMetrics)
	})
	fmt.Fprintln(r.writer)

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

func (r *Reporter) writePerformanceAnalysis(w io.Writer, profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) {
	warningColor := color.New(color.FgYellow)
	infoColor := color.New(color.FgCyan)

	hasIssues := false

	if profileData.CPUProfile != nil {
		if profileData.CPUProfile.CPUPercent > 100 {
			// Multi-core usage
			cores := profileData.CPUProfile.CPUPercent / 100
			warningColor.Fprintf(w, "• High CPU utilization detected (%.2f%% - using ~%.1f cores)\n", profileData.CPUProfile.CPUPercent, cores)
			hasIssues = true
		} else if profileData.CPUProfile.CPUPercent > 80 {
			warningColor.Fprintf(w, "• High CPU utilization detected (%.2f%%)\n", profileData.CPUProfile.CPUPercent)
			hasIssues = true
		}
	}

	// Check process metrics for issues
	for _, m := range profileData.ProcessMetrics {
		if m.CPUPercent > 90 {
			warningColor.Fprintf(w, "• Process hit >90%% CPU usage\n")
			hasIssues = true
			break
		}
	}

	if profileData.Duration > 10*time.Second {
		infoColor.Fprintf(w, "• Long running process (%s)\n", profileData.Duration)
		hasIssues = true
	}

	if !hasIssues {
		color.New(color.FgGreen).Fprintln(w, "• No significant performance anomalies detected.")
	}
}

func (r *Reporter) writeRecommendations(w io.Writer, profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) {
	// Simplified recommendations for the box view
	// Logic can be expanded as needed
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

func (r *Reporter) generateJSONReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	report := struct {
		ProfileData   *profiler.ProfileData   `json:"profile_data"`
		SystemMetrics []monitor.SystemMetrics `json:"system_metrics"`
	}{
		ProfileData:   profileData,
		SystemMetrics: systemMetrics,
	}

	encoder := json.NewEncoder(r.writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to encode JSON report: %v", err)
	}

	return nil
}

func (r *Reporter) generateHTMLReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	// Simple HTML template with embedded Chart.js
	const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Mirage Performance Report</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        body { font-family: sans-serif; margin: 20px; }
        .container { max-width: 1200px; margin: 0 auto; }
        .card { border: 1px solid #ddd; padding: 20px; margin-bottom: 20px; border-radius: 5px; }
        h1, h2 { color: #333; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 8px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background-color: #f2f2f2; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Mirage Performance Report</h1>
        
        <div class="card">
            <h2>Command Information</h2>
            <p><strong>Command:</strong> %s</p>
            <p><strong>Duration:</strong> %s</p>
            <p><strong>Exit Code:</strong> %d</p>
        </div>

        <div class="card">
            <h2>Process Metrics (Target)</h2>
            <canvas id="procCpuChart"></canvas>
            <canvas id="procMemChart"></canvas>
        </div>

        <div class="card">
            <h2>System Metrics (Total)</h2>
            <canvas id="sysCpuChart"></canvas>
            <canvas id="sysMemChart"></canvas>
        </div>
    </div>

    <script>
        const procMetrics = %s;
        const sysMetrics = %s;
        
        const labels = procMetrics.map(m => new Date(m.Timestamp).toLocaleTimeString());
        
        // Process Data
        const procCpuData = procMetrics.map(m => m.CPUPercent);
        const procMemData = procMetrics.map(m => m.MemoryRSS / 1024 / 1024); // MB

        // System Data
        const sysCpuData = sysMetrics.map(m => {
            return m.CPUPercent.reduce((a, b) => a + b, 0) / m.CPUPercent.length;
        });
        const sysMemData = sysMetrics.map(m => m.MemoryUsage.UsedPercent);

        // Process CPU Chart
        new Chart(document.getElementById('procCpuChart'), {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: 'Process CPU Usage (%%)',
                    data: procCpuData,
                    borderColor: 'rgb(54, 162, 235)',
                    tension: 0.1
                }]
            }
        });

        // Process Memory Chart
        new Chart(document.getElementById('procMemChart'), {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: 'Process RSS Memory (MB)',
                    data: procMemData,
                    borderColor: 'rgb(255, 159, 64)',
                    tension: 0.1
                }]
            }
        });

        // System CPU Chart
        new Chart(document.getElementById('sysCpuChart'), {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: 'System Average CPU Usage (%%)',
                    data: sysCpuData,
                    borderColor: 'rgb(75, 192, 192)',
                    tension: 0.1
                }]
            }
        });

        // System Memory Chart
        new Chart(document.getElementById('sysMemChart'), {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: 'System Memory Usage (%%)',
                    data: sysMemData,
                    borderColor: 'rgb(255, 99, 132)',
                    tension: 0.1
                }]
            }
        });
    </script>
</body>
</html>`

	procMetricsJSON, err := json.Marshal(profileData.ProcessMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal process metrics for HTML: %v", err)
	}

	sysMetricsJSON, err := json.Marshal(systemMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal system metrics for HTML: %v", err)
	}

	htmlContent := fmt.Sprintf(htmlTemplate,
		fmt.Sprintf("%s %v", profileData.Command, profileData.Args),
		profileData.Duration,
		profileData.ExitCode,
		string(procMetricsJSON),
		string(sysMetricsJSON),
	)

	_, err = fmt.Fprint(r.writer, htmlContent)
	return err
}

func (r *Reporter) generateCSVReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	writer := csv.NewWriter(r.writer)
	defer writer.Flush()

	// Write header
	header := []string{
		"Timestamp",
		"Process CPU %", "Process RSS", "Process VMS", "Process Read", "Process Write",
		"System CPU %", "System Mem Used", "System Mem %", "System Load 1m",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %v", err)
	}

	// We assume process metrics and system metrics are roughly aligned in time and count
	// If not, we iterate up to the min length
	count := len(profileData.ProcessMetrics)
	if len(systemMetrics) < count {
		count = len(systemMetrics)
	}

	for i := 0; i < count; i++ {
		pm := profileData.ProcessMetrics[i]
		sm := systemMetrics[i]

		var sysCpuTotal float64
		for _, c := range sm.CPUPercent {
			sysCpuTotal += c
		}
		sysCpuAvg := sysCpuTotal / float64(len(sm.CPUPercent))

		record := []string{
			pm.Timestamp.Format(time.RFC3339),
			fmt.Sprintf("%.2f", pm.CPUPercent),
			fmt.Sprintf("%d", pm.MemoryRSS),
			fmt.Sprintf("%d", pm.MemoryVMS),
			fmt.Sprintf("%d", pm.ReadBytes),
			fmt.Sprintf("%d", pm.WriteBytes),
			fmt.Sprintf("%.2f", sysCpuAvg),
			fmt.Sprintf("%d", sm.MemoryUsage.Used),
			fmt.Sprintf("%.2f", sm.MemoryUsage.UsedPercent),
			fmt.Sprintf("%.2f", sm.LoadAverage.Load1),
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %v", err)
		}
	}

	return nil
}
