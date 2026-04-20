package merge

import (
	"strings"
	"testing"
	"time"
)

func TestMergeLastWriteWinsTheirsNewer(t *testing.T) {
	result, err := Merge(LastWriteWins, nil,
		[]byte("old"), []byte("new"),
		FileMeta{Mtime: time.Unix(100, 0)},
		FileMeta{Mtime: time.Unix(200, 0)},
	)
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "new" {
		t.Errorf("expected theirs when theirs is newer, got %q", result)
	}
}

func TestMergeLastWriteWinsOursNewer(t *testing.T) {
	result, err := Merge(LastWriteWins, nil,
		[]byte("ours"), []byte("theirs"),
		FileMeta{Mtime: time.Unix(200, 0)},
		FileMeta{Mtime: time.Unix(100, 0)},
	)
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "ours" {
		t.Errorf("expected ours when ours is newer, got %q", result)
	}
}

func TestMergeLastWriteWinsEqualMtimeOursWins(t *testing.T) {
	same := FileMeta{Mtime: time.Unix(100, 0)}
	result, err := Merge(LastWriteWins, nil, []byte("ours"), []byte("theirs"), same, same)
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	// Equal mtime: ours wins (documented tie-break behavior)
	if string(result) != "ours" {
		t.Errorf("expected ours on equal mtime, got %q", result)
	}
}

func TestMergeTextNilAncestorReturnsOurs(t *testing.T) {
	result, err := Merge(TextMerge, nil, []byte("ours"), []byte("theirs"), FileMeta{}, FileMeta{})
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "ours" {
		t.Errorf("expected ours when no ancestor, got %q", result)
	}
}

func TestMergeTextNeitherChangedReturnsOurs(t *testing.T) {
	content := []byte("unchanged")
	result, err := Merge(TextMerge, content, content, content, FileMeta{}, FileMeta{})
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "unchanged" {
		t.Errorf("expected unchanged content, got %q", result)
	}
}

func TestMergeTextOnlyTheirsChangedTakesTheirs(t *testing.T) {
	ancestor := []byte("base")
	result, err := Merge(TextMerge, ancestor, []byte("base"), []byte("theirs changed"), FileMeta{}, FileMeta{})
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "theirs changed" {
		t.Errorf("expected theirs when only theirs changed, got %q", result)
	}
}

func TestMergeTextBothChangedProducesConflictMarkers(t *testing.T) {
	result, err := Merge(TextMerge, []byte("base"), []byte("ours changed"), []byte("theirs changed"), FileMeta{}, FileMeta{})
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	s := string(result)
	if !strings.Contains(s, "<<<<<<< ours") || !strings.Contains(s, ">>>>>>> theirs") {
		t.Errorf("expected conflict markers, got %q", s)
	}
	if !strings.Contains(s, "ours changed") || !strings.Contains(s, "theirs changed") {
		t.Errorf("expected both sides in conflict output, got %q", s)
	}
}

func TestMergeImmutableAlwaysReturnsOurs(t *testing.T) {
	result, err := Merge(Immutable, nil, []byte("ours"), []byte("theirs"), FileMeta{}, FileMeta{})
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if string(result) != "ours" {
		t.Errorf("expected ours for immutable strategy, got %q", result)
	}
}

func TestMergeUnknownStrategyReturnsError(t *testing.T) {
	_, err := Merge("bogus_strategy", nil, []byte("a"), []byte("b"), FileMeta{}, FileMeta{})
	if err == nil {
		t.Fatal("expected error for unknown strategy, got nil")
	}
}
