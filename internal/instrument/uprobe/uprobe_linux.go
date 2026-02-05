//go:build linux

package uprobe

import (
	"fmt"
)

func AttachUprobes(binaryPath string, symbols []string, pid int) (map[string]uint64, func(), error) {
	if len(symbols) == 0 {
		return nil, func() {}, nil
	}
	return nil, nil, fmt.Errorf("uprobes require CAP_SYS_ADMIN and a built eBPF program; see docs/INSTRUMENTATION.md: %w", ErrNotSupported)
}
