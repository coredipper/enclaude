package merge

import (
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
	const prefix = "[enclaude-merge]"

	// Optimization: avoid scanner/reader allocations and eliminate the per-line
	// map allocation for parsed fields by scanning substrings directly.
	for line := range strings.SplitSeq(combinedOutput, "\n") {
		idx := strings.Index(line, prefix)
		if idx == -1 {
			continue
		}

		rest := line[idx+len(prefix):]
		var strategy string
		var deduped, sessionsDeduped int

		for tok := range strings.FieldsSeq(rest) {
			k, v, ok := strings.Cut(tok, "=")
			if !ok || k == "" {
				continue
			}
			switch k {
			case "strategy":
				strategy = v
			case "deduped":
				deduped, _ = strconv.Atoi(v)
			case "sessions_deduped":
				sessionsDeduped, _ = strconv.Atoi(v)
			}
		}

		if strategy == "" {
			continue
		}

		agg.FilesMerged++
		agg.PerStrategy[strategy]++
		agg.LinesDeduped += deduped
		agg.SessionsDeduped += sessionsDeduped
	}
	return agg
}
