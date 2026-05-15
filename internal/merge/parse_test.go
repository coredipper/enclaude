package merge

import "testing"

// TestParseDriverLinesAggregatesAcrossStrategies verifies ParseDriverLines
// rolls up the per-file [enclaude-merge] events the merge driver writes
// to stderr during a single git pull, regardless of interleaving with
// other output.
func TestParseDriverLinesAggregatesAcrossStrategies(t *testing.T) {
	input := `Merge made by the 'recursive' strategy.
[enclaude-merge] strategy=jsonl_dedup path=projects/a/x.jsonl ours=120 theirs=130 merged=145 deduped=5
random noise
[enclaude-merge] strategy=jsonl_dedup path=projects/b/y.jsonl ours=10 theirs=12 merged=15 deduped=7
[enclaude-merge] strategy=sessions_index path=sessions-index.json sessions_added=2 sessions_deduped=1
[enclaude-merge] strategy=immutable path=projects/c/done.jsonl
 manifest.json | 8 ++--
`
	agg := ParseDriverLines(input)
	if agg.FilesMerged != 4 {
		t.Errorf("FilesMerged: want 4, got %d", agg.FilesMerged)
	}
	if agg.LinesDeduped != 12 {
		t.Errorf("LinesDeduped: want 12, got %d", agg.LinesDeduped)
	}
	if agg.SessionsDeduped != 1 {
		t.Errorf("SessionsDeduped: want 1, got %d", agg.SessionsDeduped)
	}
	if got := agg.PerStrategy["jsonl_dedup"]; got != 2 {
		t.Errorf("PerStrategy[jsonl_dedup]: want 2, got %d", got)
	}
	if got := agg.PerStrategy["immutable"]; got != 1 {
		t.Errorf("PerStrategy[immutable]: want 1, got %d", got)
	}
}

// TestParseDriverLinesIgnoresEmptyAndMalformed pins that lines without
// the prefix, or without strategy=, are skipped silently.
func TestParseDriverLinesIgnoresEmptyAndMalformed(t *testing.T) {
	input := `[enclaude-merge]
[enclaude-merge] path=foo
[enclaude-merge] strategy=jsonl_dedup
just text
`
	agg := ParseDriverLines(input)
	if agg.FilesMerged != 1 {
		t.Errorf("FilesMerged: want 1 (only strategy=... line counts), got %d", agg.FilesMerged)
	}
}

// TestParseDriverLinesEmptyInput guards the no-merge case so callers can
// rely on a zero-value Aggregate when nothing happened.
func TestParseDriverLinesEmptyInput(t *testing.T) {
	agg := ParseDriverLines("")
	if agg.FilesMerged != 0 || agg.LinesDeduped != 0 || agg.SessionsDeduped != 0 {
		t.Errorf("expected zero Aggregate, got %+v", agg)
	}
}
