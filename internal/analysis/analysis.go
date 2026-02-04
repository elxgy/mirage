package analysis

import (
	"fmt"
	"math"

	"mirage/internal/monitor"
	"mirage/internal/profiler"
)

type Resource string

const (
	ResourceCPU    Resource = "cpu"
	ResourceMemory Resource = "memory"
	ResourceIO     Resource = "io"
	ResourceSystem Resource = "system"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Finding struct {
	Resource      Resource
	Severity      Severity
	Message       string
	Evidence      string
	Recommendation string
}

type Config struct {
	CPUWarningPercent    float64
	CPUCriticalPercent   float64
	MemoryGrowthRatio    float64
	MemoryGrowthMinBytes uint64
	IOBoundCPUMax        float64
	IOBoundMinBytes      uint64
	SpikeFactor          float64
	SpikeMinSamples      int
}

var DefaultConfig = Config{
	CPUWarningPercent:    80,
	CPUCriticalPercent:   95,
	MemoryGrowthRatio:    1.2,
	MemoryGrowthMinBytes: 10 * 1024 * 1024,
	IOBoundCPUMax:        10,
	IOBoundMinBytes:      50 * 1024 * 1024,
	SpikeFactor:          2.0,
	SpikeMinSamples:      10,
}

type Analyzer struct {
	config Config
}

func NewAnalyzer(config Config) *Analyzer {
	if config.CPUWarningPercent == 0 {
		config = DefaultConfig
	}
	return &Analyzer{config: config}
}

func (a *Analyzer) Analyze(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) []Finding {
	var findings []Finding

	if profileData.CPUProfile != nil {
		if profileData.CPUProfile.CPUPercent > 100 {
			cores := profileData.CPUProfile.CPUPercent / 100
			findings = append(findings, Finding{
				Resource:      ResourceCPU,
				Severity:      SeverityInfo,
				Message:       "Multi-core CPU utilization detected",
				Evidence:      formatPercent(profileData.CPUProfile.CPUPercent) + " (~" + formatFloat(cores, 1) + " cores)",
				Recommendation: "Consider parallelization or load distribution.",
			})
		} else if profileData.CPUProfile.CPUPercent >= a.config.CPUCriticalPercent {
			findings = append(findings, Finding{
				Resource:      ResourceCPU,
				Severity:      SeverityCritical,
				Message:       "Very high single-core CPU utilization",
				Evidence:      formatPercent(profileData.CPUProfile.CPUPercent),
				Recommendation: "Optimize hot paths or consider parallelization.",
			})
		} else if profileData.CPUProfile.CPUPercent >= a.config.CPUWarningPercent {
			findings = append(findings, Finding{
				Resource:      ResourceCPU,
				Severity:      SeverityWarning,
				Message:       "High single-core CPU utilization",
				Evidence:      formatPercent(profileData.CPUProfile.CPUPercent),
				Recommendation: "Optimize hot paths or consider parallelization.",
			})
		}
	}

	if len(profileData.ProcessMetrics) > 5 {
		first := profileData.ProcessMetrics[0]
		var last monitor.ProcessMetrics
		for i := len(profileData.ProcessMetrics) - 1; i >= 0; i-- {
			if profileData.ProcessMetrics[i].MemoryRSS > 0 {
				last = profileData.ProcessMetrics[i]
				break
			}
		}
		if last.MemoryRSS > first.MemoryRSS &&
			float64(last.MemoryRSS) >= float64(first.MemoryRSS)*a.config.MemoryGrowthRatio &&
			last.MemoryRSS >= a.config.MemoryGrowthMinBytes {
			findings = append(findings, Finding{
				Resource:      ResourceMemory,
				Severity:      SeverityWarning,
				Message:       "Potential memory growth detected",
				Evidence:      formatBytes(first.MemoryRSS) + " -> " + formatBytes(last.MemoryRSS),
				Recommendation: "Check for memory leaks or inefficient object retention.",
			})
		}
	}

	var totalIO uint64
	for _, m := range profileData.ProcessMetrics {
		totalIO += m.ReadBytes + m.WriteBytes
	}
	if profileData.CPUProfile != nil &&
		profileData.CPUProfile.CPUPercent < a.config.IOBoundCPUMax &&
		totalIO >= a.config.IOBoundMinBytes {
		findings = append(findings, Finding{
			Resource:      ResourceIO,
			Severity:      SeverityInfo,
			Message:       "Process appears I/O bound",
			Evidence:      "Low CPU (" + formatPercent(profileData.CPUProfile.CPUPercent) + "), high I/O (" + formatBytes(totalIO) + ")",
			Recommendation: "Consider buffered I/O or asynchronous operations.",
		})
	}

	if profileData.SystemStats != nil && len(systemMetrics) > 0 {
		var avgLoad float64
		for _, m := range systemMetrics {
			if m.LoadAverage != nil {
				avgLoad += m.LoadAverage.Load1
			}
		}
		avgLoad /= float64(len(systemMetrics))
		cores := float64(profileData.SystemStats.CPUCount)
		if avgLoad > cores {
			findings = append(findings, Finding{
				Resource:      ResourceSystem,
				Severity:      SeverityWarning,
				Message:       "System saturation detected",
				Evidence:      "Load " + formatFloat(avgLoad, 2) + " > " + formatFloat(cores, 0) + " cores",
				Recommendation: "Host under heavy load; benchmark results may be affected.",
			})
		}
	}

	if a.config.SpikeFactor > 0 && len(profileData.ProcessMetrics) >= a.config.SpikeMinSamples {
		spike := a.detectCPUSpike(profileData.ProcessMetrics)
		if spike != nil {
			findings = append(findings, *spike)
		}
	}

	return findings
}

func (a *Analyzer) detectCPUSpike(metrics []monitor.ProcessMetrics) *Finding {
	k := a.config.SpikeMinSamples
	if k > len(metrics)/2 {
		k = len(metrics) / 2
	}
	if k < 3 {
		return nil
	}
	var sum, sumSq float64
	for i := 0; i < k; i++ {
		sum += metrics[i].CPUPercent
		sumSq += metrics[i].CPUPercent * metrics[i].CPUPercent
	}
	mean := sum / float64(k)
	variance := sumSq/float64(k) - mean*mean
	if variance <= 0 {
		return nil
	}
	stddev := math.Sqrt(variance)
	threshold := mean + a.config.SpikeFactor*stddev
	for i := k; i < len(metrics); i++ {
		if metrics[i].CPUPercent > threshold {
			return &Finding{
				Resource:      ResourceCPU,
				Severity:      SeverityInfo,
				Message:       "CPU spike detected",
				Evidence:      formatPercent(metrics[i].CPUPercent) + " (avg " + formatPercent(mean) + ", threshold " + formatPercent(threshold) + ")",
				Recommendation: "Review workload phases for bursty CPU usage.",
			}
		}
	}
	return nil
}

func formatPercent(p float64) string {
	return fmt.Sprintf("%.2f%%", p)
}

func formatFloat(v float64, decimals int) string {
	if decimals == 0 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.*f", decimals, v)
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}