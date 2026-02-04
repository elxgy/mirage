package pipeline

import (
	"time"

	"mirage/internal/analysis"
	"mirage/internal/monitor"
	"mirage/internal/profiler"
)

type Session struct {
	Command        string
	Args           []string
	StartTime      time.Time
	EndTime        time.Time
	ProfileData    *profiler.ProfileData
	SystemMetrics  []monitor.SystemMetrics
	ProcessMetrics []monitor.ProcessMetrics
	Findings       []analysis.Finding
	TracePath      string
	MutexPath      string
}

func NewSession(command string, args []string) *Session {
	return &Session{
		Command:   command,
		Args:      args,
		StartTime: time.Now(),
	}
}

func Normalize(s *Session) {
	if s.ProfileData == nil || len(s.ProfileData.ProcessMetrics) == 0 {
		return
	}
	if s.ProfileData.MemProfile == nil {
		s.ProfileData.MemProfile = &profiler.MemoryProfileData{}
	}
	for _, m := range s.ProfileData.ProcessMetrics {
		if m.MemoryRSS > s.ProfileData.MemProfile.PeakRSS {
			s.ProfileData.MemProfile.PeakRSS = m.MemoryRSS
		}
		if m.MemoryVMS > s.ProfileData.MemProfile.PeakVMS {
			s.ProfileData.MemProfile.PeakVMS = m.MemoryVMS
		}
	}
}

func Analyze(s *Session, cfg analysis.Config) {
	if s.ProfileData == nil {
		return
	}
	analyzer := analysis.NewAnalyzer(cfg)
	s.Findings = analyzer.Analyze(s.ProfileData, s.SystemMetrics)
}
