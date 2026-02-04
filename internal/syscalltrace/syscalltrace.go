package syscalltrace

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type SyscallStat struct {
	Name      string
	Count     int64
	Errors    int64
	TotalTime float64
}

func AttachAndWait(ctx context.Context, pid int) ([]SyscallStat, error) {
	stracePath, err := exec.LookPath("strace")
	if err != nil {
		return nil, fmt.Errorf("strace not found: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, stracePath, "-f", "-c", "-o", "/dev/null", "-p", strconv.Itoa(pid))
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return parseSummary(stderrBuf.Bytes())
}

func RunWithStrace(ctx context.Context, command string, args ...string) ([]SyscallStat, error) {
	stracePath, err := exec.LookPath("strace")
	if err != nil {
		return nil, fmt.Errorf("strace not found: %w", err)
	}

	straceArgs := append([]string{"-f", "-c", "-o", "/dev/null", "--"}, command)
	straceArgs = append(straceArgs, args...)

	cmd := exec.CommandContext(ctx, stracePath, straceArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return parseSummary(stderrBuf.Bytes())
}

type Session struct {
	cmd *exec.Cmd
	buf *bytes.Buffer
}

func StartAttach(pid int) (*Session, error) {
	stracePath, err := exec.LookPath("strace")
	if err != nil {
		return nil, fmt.Errorf("strace not found: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd := exec.Command(stracePath, "-f", "-c", "-o", "/dev/null", "-p", strconv.Itoa(pid))
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Session{cmd: cmd, buf: &stderrBuf}, nil
}

func (s *Session) Wait() ([]SyscallStat, error) {
	_ = s.cmd.Wait()
	return parseSummary(s.buf.Bytes())
}

func parseSummary(raw []byte) ([]SyscallStat, error) {
	var out []SyscallStat
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	var inTable bool

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "% time") || strings.HasPrefix(line, "-") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			if strings.HasPrefix(strings.TrimSpace(line), "total") {
				break
			}
			continue
		}
		seconds, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		calls, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			continue
		}
		errors, _ := strconv.ParseInt(fields[4], 10, 64)
		name := fields[5]
		out = append(out, SyscallStat{
			Name:      name,
			Count:     calls,
			Errors:    errors,
			TotalTime: seconds,
		})
	}

	return out, scanner.Err()
}
