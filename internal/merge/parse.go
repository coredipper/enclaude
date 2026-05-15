package merge

import (
	"bufio"
	"strconv"
	"strings"
)

// Aggregate is the rolled-up view of all per-file merge activity emitted
// by the enclaude merge driver during a single git pull.
type Aggregate struct {
	FilesMerged     int
	LinesDeduped    int
	SessionsDeduped int
	PerStrategy     map[string]int
}

// ParseDriverLines scans combined output from `git pull` (which captures
// the merge driver's stderr) and rolls up structured `[enclaude-merge]`
// lines into an Aggregate. Non-matching lines are ignored.
//
// Line format produced by cmd/merge_driver.go:
//
//	[enclaude-merge] strategy=<name> path=<path> [ours=N theirs=N merged=N deduped=N] [sessions_added=N sessions_deduped=N]
func ParseDriverLines(combinedOutput string) Aggregate {
	agg := Aggregate{PerStrategy: map[string]int{}}
	scanner := bufio.NewScanner(strings.NewReader(combinedOutput))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		const prefix = "[enclaude-merge]"
		_, rest, found := strings.Cut(line, prefix)
		if !found {
			continue
		}
		fields := parseFields(rest)
		strategy := fields["strategy"]
		if strategy == "" {
			continue
		}
		agg.FilesMerged++
		agg.PerStrategy[strategy]++
		agg.LinesDeduped += atoi(fields["deduped"])
		agg.SessionsDeduped += atoi(fields["sessions_deduped"])
	}
	return agg
}

// parseFields splits a sequence of `key=value` tokens separated by
// whitespace. Values may not contain spaces (we control the producer).
func parseFields(s string) map[string]string {
	out := make(map[string]string)
	for tok := range strings.FieldsSeq(s) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
