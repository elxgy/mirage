//go:build !linux

package uprobe

func AttachUprobes(binaryPath string, symbols []string, pid int) (map[string]uint64, func(), error) {
	return nil, nil, ErrNotSupported
}
