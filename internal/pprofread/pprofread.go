package pprofread

import (
	"fmt"
	"os"
	"sort"

	"github.com/google/pprof/profile"
)

type FuncCount struct {
	Name  string
	Value int64
}

func TopFunctions(path string, n int) ([]FuncCount, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse pprof: %w", err)
	}

	flat := make(map[uint64]int64)
	for _, s := range p.Sample {
		if len(s.Location) == 0 || len(s.Value) == 0 {
			continue
		}
		loc := s.Location[0]
		var v int64
		for _, val := range s.Value {
			v += val
		}
		flat[loc.ID] += v
	}

	funcTotals := make(map[string]int64)
	for locID, total := range flat {
		for _, loc := range p.Location {
			if loc.ID != locID {
				continue
			}
			for _, line := range loc.Line {
				if line.Function != nil && line.Function.Name != "" {
					funcTotals[line.Function.Name] += total
					break
				}
			}
			break
		}
	}

	var pairs []FuncCount
	for name, val := range funcTotals {
		pairs = append(pairs, FuncCount{Name: name, Value: val})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Value > pairs[j].Value })
	if n <= 0 || n > len(pairs) {
		n = len(pairs)
	}
	return pairs[:n], nil
}
