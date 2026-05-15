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

func TestConfigMergeDriverQuoting(t *testing.T) {
	tmp := t.TempDir()
	g := New(tmp)
	if err := g.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// This is a test for the quoting logic inside ConfigMergeDriver.
	// Since os.Executable() is used, the driver string will start with the test executable path.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	testArgs := []string{"merge-driver", "manifest", "has'quote", "%O", "%A", "%B"}
	if err := g.ConfigMergeDriver("testdriver", testArgs); err != nil {
		t.Fatalf("ConfigMergeDriver failed: %v", err)
	}

	// Check what was set in git config
	out, err := g.run("config", "merge.testdriver.driver")
	if err != nil {
		t.Fatalf("failed to read git config: %v", err)
	}
	out = strings.TrimSpace(out)

	// Validate quoting
	expectedExe := "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
	if !strings.HasPrefix(out, expectedExe) {
		t.Errorf("expected driver to start with %s, got %s", expectedExe, out)
	}

	if !strings.Contains(out, "'merge-driver'") {
		t.Errorf("expected 'merge-driver' to be properly quoted, got %s", out)
	}

	if !strings.Contains(out, "'has'\\''quote'") {
		t.Errorf("expected 'has'\\''quote' to be properly quoted, got %s", out)
	}

	if !strings.Contains(out, " %O %A %B") {
		t.Errorf("expected git special tokens to remain unquoted, got %s", out)
	}
}
