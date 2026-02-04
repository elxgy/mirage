//go:build linux

package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupRoot = "/sys/fs/cgroup"

func newScope(pid int) (*Scope, error) {
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		return nil, fmt.Errorf("cgroup v2 not available: %w", err)
	}

	name := fmt.Sprintf("mirage-%d", pid)
	path := filepath.Join(cgroupRoot, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("create cgroup: %w", err)
	}

	procsPath := filepath.Join(path, "cgroup.procs")
	if err := os.WriteFile(procsPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		os.RemoveAll(path)
		return nil, fmt.Errorf("assign pid to cgroup: %w", err)
	}

	return &Scope{path: path}, nil
}

func (s *Scope) readStats() (Stats, error) {
	var st Stats
	st.IOStat = make(map[string]IOStatEntry)

	if b, err := os.ReadFile(filepath.Join(s.path, "memory.current")); err == nil {
		line := strings.TrimSpace(string(b))
		if line != "max" {
			st.MemoryCurrent, _ = strconv.ParseUint(line, 10, 64)
		}
	}

	if b, err := os.ReadFile(filepath.Join(s.path, "io.stat")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			dev := parts[0]
			var e IOStatEntry
			for _, p := range parts[1:] {
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					continue
				}
				v, _ := strconv.ParseUint(kv[1], 10, 64)
				switch kv[0] {
				case "rbytes":
					e.ReadBytes = v
				case "wbytes":
					e.WriteBytes = v
				case "rios":
					e.ReadIOs = v
				case "wios":
					e.WriteIOs = v
				}
			}
			st.IOStat[dev] = e
		}
	}

	if b, err := os.ReadFile(filepath.Join(s.path, "cpu.stat")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			v, _ := strconv.ParseUint(parts[1], 10, 64)
			switch parts[0] {
			case "usage_usec":
				st.CPUStat.UsageUsec = v
			case "nr_periods":
				st.CPUStat.NrPeriods = v
			case "nr_throttled":
				st.CPUStat.NrThrottled = v
			case "throttled_usec":
				st.CPUStat.ThrottledUsec = v
			}
		}
	}

	return st, nil
}

func (s *Scope) cleanup() error {
	return os.RemoveAll(s.path)
}
