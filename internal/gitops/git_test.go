package gitops

import (
	"strings"
	"testing"
)

func TestGitCommandsArgumentInjection(t *testing.T) {
	tests := []struct {
		name string
		fn   func(g *Git) error
	}{
		{"Add", func(g *Git) error { return g.Add("--upload-pack=foo") }},
		{"Fetch", func(g *Git) error { _, err := g.Fetch("--upload-pack=foo"); return err }},
		{"Merge", func(g *Git) error { _, err := g.Merge("--upload-pack=foo"); return err }},
		// RemoteAdd doesn't error when we add a remote starting with --, it successfully adds it
		{"RemoteRemove", func(g *Git) error { return g.RemoteRemove("--upload-pack=foo") }},
		{"RemoteSetURL", func(g *Git) error { return g.RemoteSetURL("--upload-pack=foo", "http://test") }},
		{"Push", func(g *Git) error { _, _, err := g.Push("--upload-pack=foo", "main"); return err }},
		{"PushWithUpstream", func(g *Git) error { _, _, err := g.PushWithUpstream("--upload-pack=foo", "main"); return err }},
		{"Pull", func(g *Git) error { _, _, err := g.Pull("--upload-pack=foo", "main"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			g := New(tmpDir)

			if err := g.Init(); err != nil {
				t.Fatalf("failed to init repo: %v", err)
			}

			// Add a dummy commit so push/pull/merge don't immediately fail due to empty repo
			// We pass user.name and user.email inline to avoid "Author identity unknown" in CI
			if _, _, err := g.runSeparate("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init"); err != nil {
				t.Fatalf("failed to create commit: %v", err)
			}

			err := tt.fn(g)
			if err == nil {
				t.Fatalf("expected error from git %s, got nil", strings.ToLower(tt.name))
			}

			if strings.Contains(err.Error(), "129") {
				t.Fatalf("Git command failed with status 129, meaning it parsed the flag '--upload-pack=foo' instead of treating it as a positional argument.")
			}
		})
	}
}
