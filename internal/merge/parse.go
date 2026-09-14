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
	for len(combinedOutput) > 0 {
		var line string
		idx := strings.IndexByte(combinedOutput, '\n')
		if idx == -1 {
			line = combinedOutput
			combinedOutput = ""
		} else {
			line = combinedOutput[:idx]
			combinedOutput = combinedOutput[idx+1:]
		}

		prefixIdx := strings.Index(line, prefix)
		if prefixIdx == -1 {
			continue
		}

		rest := line[prefixIdx+len(prefix):]
		var strategy string
		var deduped, sessionsDeduped int

		// Fast field parsing to avoid FieldsSeq allocation overhead
		for len(rest) > 0 {
			// skip spaces
			start := 0
			for start < len(rest) && (rest[start] == ' ' || rest[start] == '\t' || rest[start] == '\r') {
				start++
			}
			if start == len(rest) {
				break
			}
			rest = rest[start:]

			// find end of token
			end := 0
			for end < len(rest) && rest[end] != ' ' && rest[end] != '\t' && rest[end] != '\r' {
				end++
			}
			tok := rest[:end]
			rest = rest[end:]

			eqIdx := strings.IndexByte(tok, '=')
			if eqIdx == -1 || eqIdx == 0 {
				continue
			}
			k, v := tok[:eqIdx], tok[eqIdx+1:]

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
