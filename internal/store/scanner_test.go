package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create a mock ~/.claude/ structure
	files := map[string]string{
		"history.jsonl":                                 `{"display":"test","timestamp":1}`,
		"settings.json":                                 `{"hooks":{}}`,
		"settings.local.json":                           `{"perms":{}}`,
		"stats-cache.json":                              `{"version":2}`,
		"CLAUDE.md":                                     "# test",
		"projects/proj-a/abc123.jsonl":                  `{"type":"user"}`,
		"projects/proj-a/sessions-index.json":           `{"entries":[]}`,
		"projects/proj-a/memory/MEMORY.md":              "# memory",
		"projects/proj-a/memory/user_role.md":           "---\nname: role\n---",
		"projects/proj-a/subagents/agent-abc.meta.json": `{"agentType":"Explore"}`,
		"projects/proj-a/subagents/agent-abc.jsonl":     `{"type":"user"}`,
		"statsig/statsig.cached.evaluations":            "big cache",
		"plugins/marketplace/plugin.json":               "{}",
		"plugins/blocklist.json":                        "{}",
		"debug/session.log":                             "log data",
		"shell-snapshots/snap1.json":                    "{}",
		"hooks/myhook.sh":                               "#!/bin/bash",
		"todos/task1/state.json":                        "{}",
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
			got := MatchGlob(tt.path, tt.pattern)
			if got != tt.want {
				t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

// oldMatchGlob and oldMatchSegments reproduce the strings.Split-based matcher
// that shipped on origin/main before matchSegments was rewritten as an
// index-walk. They are the behavioral reference: the optimized matcher must
// agree with them on every input, so the differential test below can pin the
// shipped semantics against any future regression.
func oldMatchGlob(path, pattern string) bool {
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}
	pathSegs := strings.Split(path, "/")
	patSegs := strings.Split(pattern, "/")
	return oldMatchSegments(pathSegs, patSegs)
}

func oldMatchSegments(pathSegs, patSegs []string) bool {
	for len(patSegs) > 0 {
		pat := patSegs[0]

		if pat == "**" {
			patSegs = patSegs[1:]
			if len(patSegs) == 0 {
				return true
			}
			for i := 0; i <= len(pathSegs); i++ {
				if oldMatchSegments(pathSegs[i:], patSegs) {
					return true
				}
			}
			return false
		}

		if len(pathSegs) == 0 {
			return false
		}

		matched, _ := filepath.Match(pat, pathSegs[0])
		if !matched {
			return false
		}

		pathSegs = pathSegs[1:]
		patSegs = patSegs[1:]
	}

	return len(pathSegs) == 0
}

// TestMatchGlob_DifferentialAgainstOldSplit verifies that the optimized
// index-walk matcher agrees with the shipped strings.Split-based matcher across
// a broad matrix of (path, pattern) pairs, why: PR #54's rewrite silently
// dropped the trailing empty segment that strings.Split("a/b/", "/") produces,
// flipping results for non-terminal ** against trailing-slash paths (the
// scanner deliberately probes rel+"/" for directory pruning). The known
// divergences are pinned as explicit want rows so the old behavior stays
// authoritative.
func TestMatchGlob_DifferentialAgainstOldSplit(t *testing.T) {
	paths := []string{
		"",
		"a",
		"a/",
		"a/b",
		"a/b/",
		"a/b/c",
		"a/b/c/",
		"/a",
		"/a/b",
		"projects/p/memory",
		"projects/p/memory/",
		"projects/p/memory/MEMORY.md",
		"statsig/cache.json",
		"statsig/",
		"plugins/marketplace/plugin.json",
		"b",
		"x/b",
		"x/y/b",
		"a/b/b",
	}
	patterns := []string{
		"a/**/b",
		"a/**",
		"**/b",
		"**",
		"**/*",
		"*/**",
		"**/memory",
		"projects/**/memory",
		"projects/*/memory/**",
		"a/*/b",
		"statsig/**",
		"plugins/**",
		"a/**/b/**",
		"**/**",
		"*",
		"a",
		// Pattern-side trailing-empty / empty cases: the matcher slices the
		// pattern on the fly, so these pin that it reproduces strings.Split's
		// trailing-empty ("a/" -> ["a",""]) and empty-input ("" -> [""])
		// segments rather than collapsing them.
		"a/**/",
		"**/",
		"statsig/**/",
		"",
	}

	for _, p := range paths {
		for _, pat := range patterns {
			want := oldMatchGlob(p, pat)
			got := MatchGlob(p, pat)
			if got != want {
				t.Errorf("MatchGlob(%q, %q) = %v, want %v (old split semantics)", p, pat, got, want)
			}
		}
	}
}

// TestMatchGlob_KnownDivergences guards the specific cases the review flagged
// where PR #54's rewrite diverged from origin/main; each row asserts the OLD
// (shipped) expected result so the rewrite cannot reintroduce the regression.
func TestMatchGlob_KnownDivergences(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"a/b/", "a/**/b", false},
		{"projects/p/memory/", "projects/**/memory", false},
		{"a/b/", "**/b", false},
		{"", "**/*", true},
		{"", "*/**", true},
	}

	for _, tt := range tests {
		got := MatchGlob(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}

// mkUnreadableDir creates dir/noperms with mode 0000, registers cleanup that
// restores permissions so t.TempDir() can be removed, and skips the test if
// the directory is still readable after chmod — which happens when tests run
// as root or on filesystems that don't enforce mode bits, where the
// permission-denied assertion would be a false failure.
func mkUnreadableDir(t *testing.T, dir string) string {
	t.Helper()
	noPermsDir := filepath.Join(dir, "noperms")
	if err := os.Mkdir(noPermsDir, 0000); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	t.Cleanup(func() { os.Chmod(noPermsDir, 0755) })
	if _, err := os.ReadDir(noPermsDir); err == nil {
		t.Skip("directory readable despite mode 0000 (running as root or permissive filesystem); permission test not meaningful here")
	}
	return noPermsDir
}

// TestScanFilesErrors verifies that ScanFiles handles inaccessible directories appropriately,
// correctly distinguishing between permission errors in included vs. excluded paths.
func TestScanFilesErrors(t *testing.T) {
	t.Run("PermissionDeniedIncluded", func(t *testing.T) {
		dir := t.TempDir()
		mkUnreadableDir(t, dir)

		_, err := ScanFiles(dir, []string{"**"}, nil)
		if err == nil {
			t.Fatal("expected error for inaccessible directory")
		}
		if err.Error() != "scan incomplete: 1 inaccessible file(s)" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("PermissionDeniedExcluded", func(t *testing.T) {
		dir := t.TempDir()
		mkUnreadableDir(t, dir)

		// Exclude the inaccessible directory
		excludes := []string{"noperms/**"}
		results, err := ScanFiles(dir, []string{"**"}, excludes)
		if err != nil {
			t.Fatalf("expected no error when inaccessible directory is excluded, got: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected no results, got %d", len(results))
		}
	})
}

// BenchmarkMatchGlob exercises the ** glob matcher over a representative mix of
// deep paths, non-terminal **, and trailing-slash directory probes (rel+"/"),
// covering the allocation-sensitive index-walk path so the optimization claim
// in PR #54 is backed by a real measurement.
func BenchmarkMatchGlob(b *testing.B) {
	cases := []struct{ path, pattern string }{
		{"projects/proj-a/memory/MEMORY.md", "projects/*/memory/**"},
		{"projects/proj-a/memory/", "projects/**/memory"},
		{"statsig/deep/nested/file.txt", "statsig/**"},
		{"a/b/c/d/e/f/g", "a/**/g"},
		{"a/b/", "a/**/b"},
		{"plugins/marketplace/plugin.json", "plugins/**"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink bool
	for i := 0; i < b.N; i++ {
		c := cases[i%len(cases)]
		sink = MatchGlob(c.path, c.pattern)
	}
	_ = sink
}

func TestFastRel(t *testing.T) {
	tests := []struct {
		base string
		path string
		want string
	}{
		{"/a/b", "/a/b/c", "c"},
		{"/a/b", "/a/b", "."},
		{"/a/b/", "/a/b/c", "c"},
		{"/a/b/", "/a/b/c/d", "c/d"},
		{"/a/b", "/x/y", "../../x/y"}, // Handled by fallback
	}

	for _, tt := range tests {
		t.Run(tt.base+"_"+tt.path, func(t *testing.T) {
			got, err := fastRel(tt.base, tt.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("fastRel(%q, %q) = %q, want %q", tt.base, tt.path, got, tt.want)
			}
		})
	}
}

func BenchmarkFastRel(b *testing.B) {
	base := "/my/base/dir"
	path := "/my/base/dir/subdir/file.txt"

	b.Run("NoTrailingSlash", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = fastRel(base, path)
		}
	})

	baseSlash := "/my/base/dir/"
	b.Run("WithTrailingSlash", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = fastRel(baseSlash, path)
		}
	})
}

// BenchmarkScannerMatchesAny_ExactPattern exercises the wildcard-free fast path
// in matchesAnyCompiled, where each pattern short-circuits to a string compare
// instead of filepath.Match — the path the exact-pattern optimization targets,
// which BenchmarkMatchGlob's all-wildcard cases never reach.
func BenchmarkScannerMatchesAny_ExactPattern(b *testing.B) {
	patterns := compilePatterns([]string{
		"history.jsonl",
		"settings.json",
		"settings.local.json",
		"projects/index.json",
		"todos/index.json",
		"statsig/statsig.cached.evaluations",
	})
	// Mix of a hit late in the list and a miss that scans every pattern.
	paths := []string{
		"settings.local.json",
		"statsig/statsig.cached.evaluations",
		"shell-snapshots/snapshot-zsh.sh",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink bool
	for i := 0; i < b.N; i++ {
		sink = matchesAnyCompiled(paths[i%len(paths)], patterns)
	}
	_ = sink
}
