//go:build !linux

package cgroup

import "fmt"

func newScope(pid int) (*Scope, error) {
	return nil, fmt.Errorf("cgroup isolation is Linux-only")
}

func (s *Scope) readStats() (Stats, error) {
	return Stats{}, nil
}

func (s *Scope) cleanup() error {
	return nil
}
