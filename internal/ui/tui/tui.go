package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mirage/internal/monitor"
	"mirage/internal/profiler"
)

type ProfileDoneMsg struct {
	Data *profiler.ProfileData
	Err  error
}

type tickMsg struct{}

type model struct {
	command       string
	args          []string
	processMetrics *monitor.ProcessMetrics
	systemMetrics  *monitor.SystemMetrics
	done          bool
	profileData   *profiler.ProfileData
	err           error
	metricsChan   <-chan monitor.ProcessMetrics
	getSystem     func() *monitor.SystemMetrics
	doneChan      <-chan ProfileDoneMsg
	width         int
	height        int
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sectionStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true, false, false, false).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func NewProgram(command string, args []string, metricsChan <-chan monitor.ProcessMetrics, getSystem func() *monitor.SystemMetrics, doneChan <-chan ProfileDoneMsg) *tea.Program {
	m := model{
		command:     command,
		args:        args,
		metricsChan: metricsChan,
		getSystem:   getSystem,
		doneChan:    doneChan,
	}
	return tea.NewProgram(m)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} }),
		func() tea.Msg { return <-m.doneChan },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" && m.done {
			return m, tea.Quit
		}
		if msg.String() == "q" && m.err != nil {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tickMsg:
		select {
		case pm := <-m.metricsChan:
			m.processMetrics = &pm
		default:
		}
		if m.getSystem != nil {
			m.systemMetrics = m.getSystem()
		}
		return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
	case ProfileDoneMsg:
		m.done = true
		m.profileData = msg.Data
		m.err = msg.Err
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Mirage Live"))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render(m.command + " " + strings.Join(m.args, " ")))
	b.WriteString("\n\n")

	if m.done && m.profileData != nil {
		b.WriteString(sectionStyle.Render("Run finished"))
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Duration: "))
		b.WriteString(valueStyle.Render(m.profileData.Duration.String()))
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("Exit: "))
		if m.profileData.ExitCode == 0 {
			b.WriteString(okStyle.Render(fmt.Sprintf("%d", m.profileData.ExitCode)))
		} else {
			b.WriteString(errStyle.Render(fmt.Sprintf("%d", m.profileData.ExitCode)))
		}
		b.WriteString("\n\n")
	}
	if m.err != nil {
		b.WriteString(errStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
	}

	b.WriteString(sectionStyle.Render("Process (target + children)"))
	b.WriteString("\n")
	if m.processMetrics != nil {
		b.WriteString(metricLine("CPU", fmt.Sprintf("%.1f%%", m.processMetrics.CPUPercent)))
		b.WriteString(metricLine("RSS", formatBytes(m.processMetrics.MemoryRSS)))
		b.WriteString(metricLine("VMS", formatBytes(m.processMetrics.MemoryVMS)))
		b.WriteString(metricLine("Read", formatBytes(m.processMetrics.ReadBytes)))
		b.WriteString(metricLine("Write", formatBytes(m.processMetrics.WriteBytes)))
		b.WriteString(metricLine("Threads", fmt.Sprintf("%d", m.processMetrics.ThreadCount)))
		b.WriteString(metricLine("FDs", fmt.Sprintf("%d", m.processMetrics.FileDescriptors)))
	} else {
		b.WriteString(labelStyle.Render("  waiting for metrics...\n"))
	}
	b.WriteString("\n")

	b.WriteString(sectionStyle.Render("System"))
	b.WriteString("\n")
	if m.systemMetrics != nil {
		var cpuAvg float64
		if len(m.systemMetrics.CPUPercent) > 0 {
			for _, p := range m.systemMetrics.CPUPercent {
				cpuAvg += p
			}
			cpuAvg /= float64(len(m.systemMetrics.CPUPercent))
		}
		b.WriteString(metricLine("CPU", fmt.Sprintf("%.1f%%", cpuAvg)))
		if m.systemMetrics.MemoryUsage != nil {
			b.WriteString(metricLine("Memory", fmt.Sprintf("%.1f%%", m.systemMetrics.MemoryUsage.UsedPercent)))
		}
		if m.systemMetrics.LoadAverage != nil {
			b.WriteString(metricLine("Load 1m", fmt.Sprintf("%.2f", m.systemMetrics.LoadAverage.Load1)))
		}
	} else {
		b.WriteString(labelStyle.Render("  waiting for metrics...\n"))
	}

	b.WriteString("\n")
	if m.done {
		b.WriteString(hintStyle.Render("Press q to quit"))
	} else {
		b.WriteString(hintStyle.Render("Profiling... (Ctrl+C to stop)"))
	}

	return b.String()
}

func metricLine(label, value string) string {
	return "  " + labelStyle.Render(label+":") + " " + valueStyle.Render(value) + "\n"
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
