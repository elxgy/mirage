package cgroup

type Stats struct {
	MemoryCurrent uint64
	IOStat        map[string]IOStatEntry
	CPUStat       CPUStat
}

type IOStatEntry struct {
	ReadBytes  uint64
	WriteBytes uint64
	ReadIOs    uint64
	WriteIOs   uint64
}

type CPUStat struct {
	UsageUsec uint64
	NrPeriods uint64
	NrThrottled uint64
	ThrottledUsec uint64
}

type Scope struct {
	path string
}

func (s *Scope) Path() string { return s.path }

func NewScope(pid int) (*Scope, error) {
	return newScope(pid)
}

func (s *Scope) ReadStats() (Stats, error) {
	return s.readStats()
}

func (s *Scope) Cleanup() error {
	return s.cleanup()
}
