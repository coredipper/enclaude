package store

import (
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/merge"
)

func mergeAggregateFixture() merge.Aggregate {
	return merge.Aggregate{
		FilesMerged:     2,
		LinesDeduped:    7,
		SessionsDeduped: 1,
		PerStrategy:     map[string]int{"jsonl_dedup": 2},
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range tests {
		got := FormatSize(tc.bytes)
		if got != tc.expected {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.expected)
		}
	}
}

func TestSealStatsHasChanges(t *testing.T) {
	if !(SealStats{Added: 1}).HasChanges() {
		t.Error("Added > 0 should HasChanges")
	}
	if !(SealStats{Modified: 1}).HasChanges() {
		t.Error("Modified > 0 should HasChanges")
	}
	if !(SealStats{Deleted: 1}).HasChanges() {
		t.Error("Deleted > 0 should HasChanges")
	}
	if (SealStats{Unchanged: 5}).HasChanges() {
		t.Error("only Unchanged should not HasChanges")
	}
}

func TestSealStatsStringIncludesDeletedWhenNonZero(t *testing.T) {
	s := SealStats{Scanned: 10, Added: 1, Modified: 2, Deleted: 1, Unchanged: 6}
	if !strings.Contains(s.String(), "deleted") {
		t.Errorf("expected 'deleted' when Deleted > 0, got %q", s.String())
	}
}

func TestSealStatsStringOmitsDeletedWhenZero(t *testing.T) {
	s := SealStats{Scanned: 10, Added: 1, Modified: 2, Unchanged: 7}
	if strings.Contains(s.String(), "deleted") {
		t.Errorf("expected no 'deleted' when Deleted == 0, got %q", s.String())
	}
}

func TestUnsealStatsStringIncludesDeletedWhenNonZero(t *testing.T) {
	s := UnsealStats{Total: 5, Restored: 3, Unchanged: 1, Deleted: 1}
	if !strings.Contains(s.String(), "deleted") {
		t.Errorf("expected 'deleted' when Deleted > 0, got %q", s.String())
	}
}

func TestUnsealStatsStringOmitsDeletedWhenZero(t *testing.T) {
	s := UnsealStats{Total: 5, Restored: 3, Unchanged: 2}
	if strings.Contains(s.String(), "deleted") {
		t.Errorf("expected no 'deleted' when Deleted == 0, got %q", s.String())
	}
}

// TestSealStatsMultilineIncludesBytesAndSessions verifies the multiline
// format folds in the data-volume line and session counter when present.
func TestSealStatsMultilineIncludesBytesAndSessions(t *testing.T) {
	s := SealStats{
		Scanned: 10, Added: 1, Modified: 2, Unchanged: 7,
		BytesPlaintext: 2048, BytesEncrypted: 2200,
		Sessions: SessionStats{Tracked: 3, New: 1, Updated: 1},
	}
	out := s.Multiline("    ")
	for _, want := range []string{
		"scanned 10 files",
		"2.0 KB plaintext",
		"3 sessions tracked, 1 new, 1 updated",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Multiline missing %q in %q", want, out)
		}
	}
}

// TestSealStatsMultilineOmitsSessionsWhenZero guards against printing a
// confusing "0 sessions tracked" line on systems with no Claude session
// transcripts at all.
func TestSealStatsMultilineOmitsSessionsWhenZero(t *testing.T) {
	s := SealStats{Scanned: 1, Unchanged: 1}
	if strings.Contains(s.Multiline("    "), "sessions tracked") {
		t.Errorf("Multiline should not show sessions when Tracked == 0, got %q", s.Multiline("    "))
	}
}

// TestUnsealStatsMultilineIncludesMergeLine pins the merge-activity line
// that ParseDriverLines aggregates from the merge driver's stderr.
func TestUnsealStatsMultilineIncludesMergeLine(t *testing.T) {
	s := UnsealStats{
		Total: 5, Restored: 5, BytesDecrypted: 1024,
		Merges: mergeAggregateFixture(),
	}
	out := s.Multiline("    ")
	if !strings.Contains(out, "merged 2 files") {
		t.Errorf("expected merge summary line, got %q", out)
	}
	if !strings.Contains(out, "7 dup lines") {
		t.Errorf("expected dup line count, got %q", out)
	}
}
