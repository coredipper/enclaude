package merge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMergeJSONLDeduplicate(t *testing.T) {
	ours := []byte(`{"display":"prompt 1","timestamp":1000}
{"display":"prompt 2","timestamp":2000}
{"display":"prompt 3","timestamp":3000}
`)
	theirs := []byte(`{"display":"prompt 2","timestamp":2000}
{"display":"prompt 3","timestamp":3000}
{"display":"prompt 4","timestamp":4000}
`)

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), string(merged))
	}

	// Verify order (should be sorted by timestamp)
	if !strings.Contains(lines[0], `"prompt 1"`) {
		t.Errorf("line 0 should be prompt 1, got: %s", lines[0])
	}
	if !strings.Contains(lines[3], `"prompt 4"`) {
		t.Errorf("line 3 should be prompt 4, got: %s", lines[3])
	}
}

func TestMergeJSONLNoOverlap(t *testing.T) {
	ours := []byte(`{"display":"a","timestamp":1000}
`)
	theirs := []byte(`{"display":"b","timestamp":2000}
`)

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestMergeJSONLCompleteOverlap(t *testing.T) {
	data := []byte(`{"display":"same","timestamp":1000}
`)

	merged, err := MergeJSONL(data, data)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 1 {
		t.Fatalf("expected 1 line after dedup, got %d", len(lines))
	}
}

func TestMergeJSONLEmptyInputs(t *testing.T) {
	merged, err := MergeJSONL([]byte{}, []byte(`{"display":"x","timestamp":1}`+"\n"))
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}
	lines := nonEmptyLines(string(merged))
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestMergeJSONLWhitespaceDifference(t *testing.T) {
	// Same JSON content but different formatting — should deduplicate
	ours := []byte(`{"display":"test","timestamp":1000}` + "\n")
	theirs := []byte(`{"timestamp":1000,"display":"test"}` + "\n") // different key order

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 1 {
		t.Fatalf("expected 1 line after semantic dedup, got %d", len(lines))
	}
}

func TestMergeSessionsIndex(t *testing.T) {
	ours := []byte(`{"entries":[
		{"sessionId":"aaa","summary":"first"},
		{"sessionId":"bbb","summary":"second"}
	]}`)
	theirs := []byte(`{"entries":[
		{"sessionId":"bbb","summary":"second"},
		{"sessionId":"ccc","summary":"third"}
	]}`)

	merged, err := MergeSessionsIndex(ours, theirs)
	if err != nil {
		t.Fatalf("MergeSessionsIndex() error: %v", err)
	}

	// Should have 3 unique entries
	if !strings.Contains(string(merged), `"aaa"`) {
		t.Error("missing session aaa")
	}
	if !strings.Contains(string(merged), `"bbb"`) {
		t.Error("missing session bbb")
	}
	if !strings.Contains(string(merged), `"ccc"`) {
		t.Error("missing session ccc")
	}
}

func TestMergeStrategies(t *testing.T) {
	t.Run("last_write_wins_ours_newer", func(t *testing.T) {
		result, err := mergeLastWriteWins(
			[]byte("ours content"), []byte("theirs content"),
			FileMeta{Mtime: parseTime("2026-04-05T10:00:00Z")},
			FileMeta{Mtime: parseTime("2026-04-04T10:00:00Z")},
		)
		if err != nil {
			t.Fatal(err)
		}
		if string(result) != "ours content" {
			t.Errorf("expected ours, got: %s", result)
		}
	})

	t.Run("last_write_wins_theirs_newer", func(t *testing.T) {
		result, err := mergeLastWriteWins(
			[]byte("ours content"), []byte("theirs content"),
			FileMeta{Mtime: parseTime("2026-04-04T10:00:00Z")},
			FileMeta{Mtime: parseTime("2026-04-05T10:00:00Z")},
		)
		if err != nil {
			t.Fatal(err)
		}
		if string(result) != "theirs content" {
			t.Errorf("expected theirs, got: %s", result)
		}
	})

	t.Run("text_merge_only_theirs_changed", func(t *testing.T) {
		ancestor := []byte("original")
		ours := []byte("original")
		theirs := []byte("modified")

		result, err := mergeText(ancestor, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if string(result) != "modified" {
			t.Errorf("expected theirs, got: %s", result)
		}
	})

	t.Run("text_merge_both_changed", func(t *testing.T) {
		ancestor := []byte("original")
		ours := []byte("our change")
		theirs := []byte("their change")

		result, err := mergeText(ancestor, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(result), "<<<<<<<") {
			t.Error("expected conflict markers for both-changed case")
		}
	})
}

func nonEmptyLines(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestMergeJSONLNonJSONLines(t *testing.T) {
	ours := []byte("not valid json\n")
	theirs := []byte("also not json\n")

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}
	lines := nonEmptyLines(string(merged))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), string(merged))
	}
}

func TestMergeJSONLNonJSONDeduplicated(t *testing.T) {
	line := "not valid json"
	ours := []byte(line + "\n")
	theirs := []byte(line + "\n")

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}
	lines := nonEmptyLines(string(merged))
	if len(lines) != 1 {
		t.Fatalf("identical non-JSON lines should deduplicate to 1, got %d", len(lines))
	}
}

func TestMergeJSONLISOTimestampsChronologicalOrder(t *testing.T) {
	// ours has events a (Jan 1) and b (Jan 2); theirs has event c (Jan 3).
	// After merge the three events must appear in chronological order
	// regardless of which side each came from.
	ours := []byte(`{"event":"a","timestamp":"2024-01-01T00:00:00Z"}
{"event":"b","timestamp":"2024-01-02T00:00:00Z"}
`)
	theirs := []byte(`{"event":"c","timestamp":"2024-01-03T00:00:00Z"}
`)

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), string(merged))
	}

	// Verify chronological order by checking the "event" field sequence.
	for i, want := range []string{"a", "b", "c"} {
		var obj map[string]string
		if err := json.Unmarshal([]byte(lines[i]), &obj); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if obj["event"] != want {
			t.Errorf("line %d: got event=%q, want %q (full output: %s)", i, obj["event"], want, string(merged))
		}
	}
}

func TestMergeJSONLISOTimestampsInterleavedChronologicalOrder(t *testing.T) {
	// Events interleaved across ours/theirs — Jan 1 in theirs, Jan 2 in ours.
	// The sort must produce chronological order, not arrival order.
	ours := []byte(`{"event":"b","timestamp":"2024-01-02T00:00:00Z"}
`)
	theirs := []byte(`{"event":"a","timestamp":"2024-01-01T00:00:00Z"}
`)

	merged, err := MergeJSONL(ours, theirs)
	if err != nil {
		t.Fatalf("MergeJSONL() error: %v", err)
	}

	lines := nonEmptyLines(string(merged))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), string(merged))
	}

	var first map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 0 is not valid JSON: %v", err)
	}
	if first["event"] != "a" {
		t.Errorf("expected earlier event first, got event=%q (full output: %s)", first["event"], string(merged))
	}
}

func TestMergeSessionsIndexMissingEntriesKey(t *testing.T) {
	ours := []byte(`{"version": 1}`)
	theirs := []byte(`{"version": 1, "entries": [{"sessionId": "s1"}]}`)

	merged, err := MergeSessionsIndex(ours, theirs)
	if err != nil {
		t.Fatalf("MergeSessionsIndex() error: %v", err)
	}
	if !strings.Contains(string(merged), "s1") {
		t.Error("expected entry from theirs in merged output when ours has no entries key")
	}
}

func TestMergeSessionsIndexNoSessionIdFallsBackToFullJSON(t *testing.T) {
	ours := []byte(`{"entries": [{"name": "no-id-entry-a"}]}`)
	theirs := []byte(`{"entries": [{"name": "no-id-entry-b"}]}`)

	merged, err := MergeSessionsIndex(ours, theirs)
	if err != nil {
		t.Fatalf("MergeSessionsIndex() error: %v", err)
	}
	s := string(merged)
	if !strings.Contains(s, "no-id-entry-a") {
		t.Error("expected no-id-entry-a in merged output")
	}
	if !strings.Contains(s, "no-id-entry-b") {
		t.Error("expected no-id-entry-b in merged output")
	}
}

func TestMergeSessionsIndexDeduplicatesOnSessionId(t *testing.T) {
	entry := `{"sessionId":"same-id","name":"session"}`
	ours := []byte(`{"entries": [` + entry + `]}`)
	theirs := []byte(`{"entries": [` + entry + `]}`)

	merged, err := MergeSessionsIndex(ours, theirs)
	if err != nil {
		t.Fatalf("MergeSessionsIndex() error: %v", err)
	}
	count := strings.Count(string(merged), "same-id")
	if count != 1 {
		t.Errorf("expected sessionId to appear once, got %d times", count)
	}
}
