package preload

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Env(soPath, outPath string) ([]string, error) {
	if soPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("preload: cannot find executable: %w", err)
		}
		soPath = filepath.Join(filepath.Dir(exe), "preload_shim.so")
	}
	if _, err := os.Stat(soPath); err != nil {
		return nil, nil
	}
	return []string{
		"LD_PRELOAD=" + soPath,
		"MIRAGE_PRELOAD_OUT=" + outPath,
	}, nil
}

func ParseOutput(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]int64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out[fields[0]] = n
	}
	return out, scanner.Err()
}
