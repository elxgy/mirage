package symbolicate

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func Resolve(binary string, addresses []uint64) (map[uint64]string, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	path, err := exec.LookPath("addr2line")
	if err != nil {
		return nil, fmt.Errorf("addr2line not found: %w", err)
	}

	args := []string{"-e", binary, "-f", "-s"}
	for _, a := range addresses {
		args = append(args, "0x"+strconv.FormatUint(a, 16))
	}
	cmd := exec.Command(path, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := make(map[uint64]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for i := 0; i < len(addresses) && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "??" || line == "" {
			line = "?"
		}
		result[addresses[i]] = line
	}
	return result, scanner.Err()
}

func ResolveSlice(binary string, addresses []uint64) ([]string, error) {
	m, err := Resolve(binary, addresses)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(addresses))
	for i, a := range addresses {
		out[i] = m[a]
	}
	return out, nil
}
