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
		if err == nil {
			t.Fatalf("RemoteAdd created remote with dashed name, expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("Expected rejection due to dash, got: %v", err)
		}
	})
}

// TestRemoteRejectsExtTransport guards the ext:: git transport, which
// executes an arbitrary command on fetch/push. The -- option separator
// cannot neutralize this because the URL is a valid positional, not an
// option. Every path that records a remote URL must reject it — guarding
// only RemoteAdd would leave the set-url edit path as a bypass (add a
// benign remote, then point it at ext:: afterwards).
func TestRemoteRejectsExtTransport(t *testing.T) {
	const extURL = "ext::sh -c 'touch pwned'"

	t.Run("RemoteAdd", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		err := g.RemoteAdd("evil", extURL)
		if err == nil {
			t.Fatal("RemoteAdd accepted an ext:: URL; expected rejection")
		}
		if !strings.Contains(err.Error(), "ext::") {
			t.Errorf("rejection error should mention ext::, got: %v", err)
		}
		// A benign URL must still be accepted (records config only, no
		// network), so the guard doesn't over-reject.
		if err := g.RemoteAdd("origin", "https://example.invalid/r.git"); err != nil {
			t.Fatalf("RemoteAdd rejected a benign https URL: %v", err)
		}
	})

	t.Run("RemoteSetURL bypass is closed", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		// Bypass attempt: add a benign remote, then edit it to ext::.
		if err := g.RemoteAdd("origin", "https://example.invalid/r.git"); err != nil {
			t.Fatalf("benign RemoteAdd failed: %v", err)
		}
		err := g.RemoteSetURL("origin", extURL)
		if err == nil {
			t.Fatal("RemoteSetURL accepted an ext:: URL; the add-then-edit bypass is open")
		}
		if !strings.Contains(err.Error(), "ext::") {
			t.Errorf("rejection error should mention ext::, got: %v", err)
		}
		// A benign re-point must still work.
		if err := g.RemoteSetURL("origin", "https://example.invalid/r2.git"); err != nil {
			t.Fatalf("RemoteSetURL rejected a benign https URL: %v", err)
		}
	})
}

// TestRemoteRejectsDashPrefixedArgs covers the dash-prefix input guards: a
// remote name or URL beginning with "-" is refused before reaching git, so
// it can't be mistaken for a flag even on a git build that mishandles the --
// separator. Mirrors the ext:: coverage — every recording path is checked,
// plus benign inputs to prove the guard doesn't over-reject.
func TestRemoteRejectsDashPrefixedArgs(t *testing.T) {
	const dashName = "--upload-pack=exploit"
	const dashURL = "--upload-pack=exploit"
	const benignURL = "https://example.invalid/r.git"

	t.Run("RemoteAdd rejects dashed name", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		err := g.RemoteAdd(dashName, benignURL)
		if err == nil {
			t.Fatal("RemoteAdd accepted a dash-prefixed name; expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("rejection error should mention dash, got: %v", err)
		}
	})

	t.Run("RemoteAdd rejects dashed URL", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		err := g.RemoteAdd("origin", dashURL)
		if err == nil {
			t.Fatal("RemoteAdd accepted a dash-prefixed URL; expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("rejection error should mention dash, got: %v", err)
		}
	})

	t.Run("RemoteSetURL rejects dashed name and URL", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		if err := g.RemoteAdd("origin", benignURL); err != nil {
			t.Fatalf("benign RemoteAdd failed: %v", err)
		}
		// Assert the guard fired (error mentions "dash") rather than git
		// merely erroring because no remote named dashName exists — otherwise
		// the name case would pass even with the guard removed.
		err := g.RemoteSetURL(dashName, benignURL)
		if err == nil {
			t.Fatal("RemoteSetURL accepted a dash-prefixed name; expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("rejection error should mention dash, got: %v", err)
		}
		err = g.RemoteSetURL("origin", dashURL)
		if err == nil {
			t.Fatal("RemoteSetURL accepted a dash-prefixed URL; expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("rejection error should mention dash, got: %v", err)
		}
	})

	t.Run("RemoteRemove rejects dashed name", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		err := g.RemoteRemove(dashName)
		if err == nil {
			t.Fatal("RemoteRemove accepted a dash-prefixed name; expected rejection")
		}
		if !strings.Contains(err.Error(), "dash") {
			t.Errorf("rejection error should mention dash, got: %v", err)
		}
	})

	t.Run("benign name and URL still accepted", func(t *testing.T) {
		g := New(t.TempDir())
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		if err := g.RemoteAdd("origin", benignURL); err != nil {
			t.Fatalf("RemoteAdd rejected a benign remote: %v", err)
		}
		if err := g.RemoteSetURL("origin", "https://example.invalid/r2.git"); err != nil {
			t.Fatalf("RemoteSetURL rejected a benign re-point: %v", err)
		}
		if err := g.RemoteRemove("origin"); err != nil {
			t.Fatalf("RemoteRemove rejected a benign remote: %v", err)
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

// TestMerge covers Merge and MergeAbort: a clean fast-forward-style merge
// applies the branch's changes to the working tree, while a conflicting
// merge fails with a CONFLICT message and MergeAbort restores the pre-merge
// file state.
func TestMerge(t *testing.T) {
	tmpDir := t.TempDir()

	g := New(tmpDir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}

	// Configure git for CI environments where user is not set
	if _, err := g.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	f := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(f, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := g.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("initial commit"); err != nil {
		t.Fatal(err)
	}

	// Make sure we are on 'main' branch
	if _, err := g.run("branch", "-M", "main"); err != nil {
		t.Fatal(err)
	}

	t.Run("Clean merge", func(t *testing.T) {
		if _, err := g.run("checkout", "-b", "feature-clean"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("initial\nclean-update\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(); err != nil {
			t.Fatal(err)
		}
		if err := g.Commit("clean update"); err != nil {
			t.Fatal(err)
		}

		if _, err := g.run("checkout", "main"); err != nil {
			t.Fatal(err)
		}

		out, err := g.Merge("feature-clean")
		if err != nil {
			t.Fatalf("expected successful merge, got err: %v, out: %s", err, out)
		}

		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "initial\nclean-update\n" {
			t.Fatalf("unexpected content after clean merge: %s", string(content))
		}
	})

	t.Run("Merge conflict and abort", func(t *testing.T) {
		// We are currently on 'main'
		if _, err := g.run("checkout", "-b", "feature-conflict"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("initial\nclean-update\nfeature-conflict-update\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(); err != nil {
			t.Fatal(err)
		}
		if err := g.Commit("feature conflict update"); err != nil {
			t.Fatal(err)
		}

		if _, err := g.run("checkout", "main"); err != nil {
			t.Fatal(err)
		}

		// Make a conflicting change on main
		if err := os.WriteFile(f, []byte("initial\nclean-update\nmain-conflict-update\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(); err != nil {
			t.Fatal(err)
		}
		if err := g.Commit("main conflict update"); err != nil {
			t.Fatal(err)
		}

		out, err := g.Merge("feature-conflict")
		if err == nil {
			t.Fatalf("expected merge conflict error, got nil")
		}
		if !strings.Contains(out, "CONFLICT") {
			t.Errorf("expected conflict message in output, got: %s", out)
		}

		// Now abort the merge
		if err := g.MergeAbort(); err != nil {
			t.Fatalf("expected successful merge abort, got err: %v", err)
		}

		// Verify file state is back to pre-merge main state
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "initial\nclean-update\nmain-conflict-update\n" {
			t.Fatalf("unexpected content after merge abort: %s", string(content))
		}
	})
}

// TestGitFetch verifies Fetch populates remote-tracking refs from a
// local-path remote on success and returns an error for a nonexistent
// remote.
func TestGitFetch(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup upstream repo
	upstreamDir := filepath.Join(tmpDir, "upstream")
	if err := os.MkdirAll(upstreamDir, 0755); err != nil {
		t.Fatal(err)
	}
	upstreamGit := New(upstreamDir)
	if err := upstreamGit.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := upstreamGit.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := upstreamGit.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	// Create a commit in upstream so there's something to fetch
	f := filepath.Join(upstreamDir, "file.txt")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := upstreamGit.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := upstreamGit.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	// Normalize the branch name so the fetch assertion is deterministic
	// regardless of the environment's init.defaultBranch (e.g. trunk).
	if _, err := upstreamGit.run("branch", "-M", "main"); err != nil {
		t.Fatal(err)
	}

	// Setup local repo
	localDir := filepath.Join(tmpDir, "local")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	localGit := New(localDir)
	if err := localGit.Init(); err != nil {
		t.Fatal(err)
	}

	// Add upstream as remote
	if err := localGit.RemoteAdd("origin", upstreamDir); err != nil {
		t.Fatal(err)
	}

	// Test successful fetch
	t.Run("Success", func(t *testing.T) {
		out, err := localGit.Fetch("origin")
		if err != nil {
			t.Fatalf("Fetch failed: %v\nOutput: %s", err, out)
		}

		// Verify branches were fetched: the upstream branch was normalized to
		// main above, so origin/main must be present after the fetch.
		branches, err := localGit.run("branch", "-r")
		if err != nil {
			t.Fatalf("Failed to list remote branches: %v", err)
		}
		if !strings.Contains(branches, "origin/main") {
			t.Errorf("Expected origin/main in remote branches list, got: %q", branches)
		}
	})

	// Test error case with nonexistent remote
	t.Run("NonexistentRemote", func(t *testing.T) {
		out, err := localGit.Fetch("does-not-exist")
		if err == nil {
			t.Fatalf("Expected error when fetching from nonexistent remote, got success. Output: %s", out)
		}
	})
}

// TestGitInit covers Init: it creates the .git directory, is idempotent
// when run twice on the same repo, and returns an error when the target
// path is an existing file rather than a directory.
func TestGitInit(t *testing.T) {
	t.Run("HappyPath", func(t *testing.T) {
		tmpDir := t.TempDir()
		g := New(tmpDir)
		if err := g.Init(); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Verify .git directory is created
		gitDir := filepath.Join(tmpDir, ".git")
		info, err := os.Stat(gitDir)
		if err != nil {
			t.Fatalf("Failed to stat .git directory: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("Expected .git to be a directory")
		}
	})

	t.Run("Idempotency", func(t *testing.T) {
		tmpDir := t.TempDir()
		g := New(tmpDir)
		if err := g.Init(); err != nil {
			t.Fatalf("First Init failed: %v", err)
		}

		// Second Init should also succeed
		if err := g.Init(); err != nil {
			t.Fatalf("Second Init failed, not idempotent: %v", err)
		}
	})

	t.Run("ErrorCondition", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a file where the git repo root should be
		// Wait, New(dir) doesn't take the .git folder, it takes the working directory.
		// So to cause git init to fail, we can make the working directory itself a file.

		filePath := filepath.Join(tmpDir, "repo-file")
		if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		g := New(filePath)
		err := g.Init()
		if err == nil {
			t.Fatal("Expected Init to fail when directory path is an existing file, but it succeeded")
		}
	})
}
