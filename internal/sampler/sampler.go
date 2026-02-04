package sampler

import (
	"context"
	"sync"
	"time"
)

type Sample struct {
	Timestamp time.Time
	PID       int
	TID       int
	Syscall   string
}

type Sampler struct {
	rootPID  int
	interval time.Duration
	mu       sync.Mutex
	samples  []Sample
	stop     chan struct{}
}

func New(rootPID int, interval time.Duration) *Sampler {
	return &Sampler{
		rootPID:  rootPID,
		interval: interval,
		samples:  make([]Sample, 0, 4096),
		stop:     make(chan struct{}),
	}
}

func (s *Sampler) Samples() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Sample, len(s.samples))
	copy(out, s.samples)
	return out
}

func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.collect()
		}
	}
}

func (s *Sampler) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

func TopSyscalls(samples []Sample, n int) []SyscallCount {
	counts := make(map[string]int64)
	for _, smp := range samples {
		counts[smp.Syscall]++
	}
	type pair struct {
		name  string
		count int64
	}
	var pairs []pair
	for name, count := range counts {
		pairs = append(pairs, pair{name, count})
	}
	for i := 0; i < len(pairs)-1; i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].count > pairs[i].count {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	if n <= 0 || n > len(pairs) {
		n = len(pairs)
	}
	out := make([]SyscallCount, 0, n)
	for i := 0; i < n && i < len(pairs); i++ {
		out = append(out, SyscallCount{Name: pairs[i].name, Count: pairs[i].count})
	}
	return out
}

type SyscallCount struct {
	Name  string
	Count int64
}
