package store

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create a mock ~/.claude/ structure
	files := map[string]string{
		"history.jsonl":                                   `{"display":"test","timestamp":1}`,
		"settings.json":                                   `{"hooks":{}}`,
		"settings.local.json":                             `{"perms":{}}`,
		"stats-cache.json":                                `{"version":2}`,
		"CLAUDE.md":                                       "# test",
		"projects/proj-a/abc123.jsonl":                    `{"type":"user"}`,
		"projects/proj-a/sessions-index.json":             `{"entries":[]}`,
		"projects/proj-a/memory/MEMORY.md":                "# memory",
		"projects/proj-a/memory/user_role.md":             "---\nname: role\n---",
		"projects/proj-a/subagents/agent-abc.meta.json":   `{"agentType":"Explore"}`,
		"projects/proj-a/subagents/agent-abc.jsonl":       `{"type":"user"}`,
		"statsig/statsig.cached.evaluations":              "big cache",
		"plugins/marketplace/plugin.json":                 "{}",
		"plugins/blocklist.json":                          "{}",
		"debug/session.log":                               "log data",
		"shell-snapshots/snap1.json":                      "{}",
		"hooks/myhook.sh":                                 "#!/bin/bash",
		"todos/task1/state.json":                          "{}",
	}

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	return dir
}

func TestScanFilesDefaultConfig(t *testing.T) {
	dir := setupTestDir(t)

	includes := []string{
		"history.jsonl",
		"settings.json",
		"stats-cache.json",
		"CLAUDE.md",
		"projects/*/sessions-index.json",
		"projects/*/*.jsonl",
		"projects/*/memory/**",
		"projects/*/subagents/**",
	}
	excludes := []string{
		"statsig/**",
		"plugins/**",
		"debug/**",
		"shell-snapshots/**",
		"hooks/**",
		"todos/**",
		"settings.local.json",
	}

	results, err := ScanFiles(dir, includes, excludes)
	if err != nil {
		t.Fatalf("ScanFiles() error: %v", err)
	}

	found := make(map[string]bool)
	for _, r := range results {
		found[r.RelPath] = true
	}

	// Should include
	shouldInclude := []string{
		"history.jsonl",
		"settings.json",
		"stats-cache.json",
		"CLAUDE.md",
		"projects/proj-a/abc123.jsonl",
		"projects/proj-a/sessions-index.json",
		"projects/proj-a/memory/MEMORY.md",
		"projects/proj-a/memory/user_role.md",
		"projects/proj-a/subagents/agent-abc.meta.json",
		"projects/proj-a/subagents/agent-abc.jsonl",
	}
	for _, path := range shouldInclude {
		if !found[path] {
			t.Errorf("expected %s to be included, but it wasn't", path)
		}
	}

	// Should exclude
	shouldExclude := []string{
		"settings.local.json",
		"statsig/statsig.cached.evaluations",
		"plugins/marketplace/plugin.json",
		"plugins/blocklist.json",
		"debug/session.log",
		"shell-snapshots/snap1.json",
		"hooks/myhook.sh",
		"todos/task1/state.json",
	}
	for _, path := range shouldExclude {
		if found[path] {
			t.Errorf("expected %s to be excluded, but it was included", path)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"history.jsonl", "history.jsonl", true},
		{"settings.json", "settings.json", true},
		{"settings.local.json", "settings.local.json", true},
		{"settings.json", "settings.local.json", false},

		// ** patterns
		{"statsig/cache.json", "statsig/**", true},
		{"statsig/deep/nested/file.txt", "statsig/**", true},
		{"plugins/blocklist.json", "plugins/**", true},

		// Pattern with prefix and suffix
		{"projects/proj-a/abc.jsonl", "projects/*/*.jsonl", true},
		{"projects/proj-a/sessions-index.json", "projects/*/sessions-index.json", true},
		{"projects/proj-a/memory/MEMORY.md", "projects/*/memory/**", true},
		{"projects/proj-a/memory/deep/file.md", "projects/*/memory/**", true},

		// Wildcard extension
		{"CLAUDE.md", "*.md", true},
		{"file.lock", "*.lock", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.pattern, func(t *testing.T) {
			got := matchGlob(tt.path, tt.pattern)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}
