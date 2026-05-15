package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitOptionInjectionMitigation(t *testing.T) {
	tmpDir := t.TempDir()

	// Init base repo
	baseDir := filepath.Join(tmpDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatal(err)
	}
	baseGit := New(baseDir)
	if err := baseGit.Init(); err != nil {
		t.Fatal(err)
	}
	// Configure git for CI environments where user is not set
	if _, err := baseGit.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := baseGit.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	f := filepath.Join(baseDir, "file.txt")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := baseGit.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := baseGit.Commit("initial"); err != nil {
		t.Fatal(err)
	}

	// Test Add with dashed file
	t.Run("Add dashed file", func(t *testing.T) {
		dashedFile := filepath.Join(baseDir, "-f")
		if err := os.WriteFile(dashedFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		err := baseGit.Add("-f")
		if err != nil {
			t.Fatalf("Add failed, possibly interpreted as flag: %v", err)
		}

		// Verify '-f' is actually added by checking git status
		out, err := baseGit.run("status", "--porcelain")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "A  -f") {
			t.Errorf("Expected dashed file to be added, got status: %q", out)
		}
	})

	// Test remote with dashed name
	t.Run("RemoteAdd dashed name", func(t *testing.T) {
		err := baseGit.RemoteAdd("--upload-pack=exploit", "http://example.com")
		if err != nil {
			t.Fatalf("RemoteAdd failed to create remote with dashed name: %v", err)
		}

		// Verify remote was created with the literal dashed name
		out, err := baseGit.RemoteList()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "--upload-pack=exploit") {
			t.Errorf("Expected remote to be created with dashed name, got: %q", out)
		}
	})
}
