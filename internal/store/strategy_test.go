package store

import "testing"

func TestResolveMergeStrategyExactMatchWins(t *testing.T) {
	strategies := map[string]string{
		"history.jsonl": "jsonl_dedup",
		"**/*.jsonl":    "last_write_wins",
	}
	strategy, pattern := ResolveMergeStrategyWithPattern("history.jsonl", strategies)
	if strategy != "jsonl_dedup" {
		t.Errorf("expected jsonl_dedup, got %s", strategy)
	}
	if pattern != "history.jsonl" {
		t.Errorf("expected exact pattern, got %q", pattern)
	}
}

func TestResolveMergeStrategyMostSpecificGlobWins(t *testing.T) {
	strategies := map[string]string{
		"projects/*/sessions-index.json": "sessions_index",
		"projects/*/*.json":              "last_write_wins",
		"**/*.json":                      "text_merge",
	}
	strategy, pattern := ResolveMergeStrategyWithPattern("projects/abc/sessions-index.json", strategies)
	if strategy != "sessions_index" {
		t.Errorf("expected sessions_index (most specific), got %s", strategy)
	}
	if pattern != "projects/*/sessions-index.json" {
		t.Errorf("expected projects/*/sessions-index.json, got %q", pattern)
	}
}

func TestResolveMergeStrategyDefaultMD(t *testing.T) {
	strategies := map[string]string{
		"history.jsonl": "jsonl_dedup",
	}
	strategy, pattern := ResolveMergeStrategyWithPattern("CLAUDE.md", strategies)
	if strategy != "text_merge" {
		t.Errorf("expected text_merge for .md default, got %s", strategy)
	}
	if pattern != "" {
		t.Errorf("expected empty pattern for built-in default, got %q", pattern)
	}
}

func TestResolveMergeStrategyDefaultLastWriteWins(t *testing.T) {
	strategy, pattern := ResolveMergeStrategyWithPattern("some-random-file.txt", map[string]string{})
	if strategy != "last_write_wins" {
		t.Errorf("expected last_write_wins for unknown file, got %s", strategy)
	}
	if pattern != "" {
		t.Errorf("expected empty pattern for built-in default, got %q", pattern)
	}
}

func TestPatternSpecificityLiteralBeatsWildcard(t *testing.T) {
	literal := patternSpecificity("history.jsonl")
	wildcard := patternSpecificity("**/*.jsonl")
	if literal <= wildcard {
		t.Errorf("literal score %q should beat wildcard score %q", literal, wildcard)
	}
}

func TestPatternSpecificityDeeperBeatsShallower(t *testing.T) {
	deeper := patternSpecificity("projects/*/sessions-index.json")
	shallower := patternSpecificity("*/*.json")
	if deeper <= shallower {
		t.Errorf("deeper pattern %q should beat shallower %q", deeper, shallower)
	}
}
