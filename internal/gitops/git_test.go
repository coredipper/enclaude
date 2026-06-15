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

// TestPull verifies Pull's reported stats against a local-path upstream:
// it returns UpToDate when no new commits exist, and otherwise reports the
// fetched Commits and FilesChanged counts after the upstream advances.
func TestPull(t *testing.T) {
	tmp := t.TempDir()

	// Upstream repo
	upstreamDir := filepath.Join(tmp, "upstream")
	if err := os.MkdirAll(upstreamDir, 0755); err != nil {
		t.Fatal(err)
	}
	upstream := New(upstreamDir)
	if err := upstream.Init(); err != nil {
		t.Fatal(err)
	}
	upstream.run("config", "user.name", "Test User")
	upstream.run("config", "user.email", "test@example.com")
	upstream.run("config", "receive.denyCurrentBranch", "ignore")

	file1 := filepath.Join(upstreamDir, "file1.txt")
	os.WriteFile(file1, []byte("content1"), 0644)
	if err := upstream.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Commit("initial commit"); err != nil {
		t.Fatal(err)
	}

	// Downstream repo
	downstreamDir := filepath.Join(tmp, "downstream")
	if err := os.MkdirAll(downstreamDir, 0755); err != nil {
		t.Fatal(err)
	}
	downstream := New(downstreamDir)
	if err := downstream.Init(); err != nil {
		t.Fatal(err)
	}
	downstream.run("config", "user.name", "Test User 2")
	downstream.run("config", "user.email", "test2@example.com")
	// Force fast-forward-only pulls so the fetched-commit count is deterministic
	// regardless of the caller's global pull.ff / merge configuration.
	if _, err := downstream.run("config", "pull.ff", "only"); err != nil {
		t.Fatal(err)
	}

	if err := downstream.RemoteAdd("origin", upstreamDir); err != nil {
		t.Fatal(err)
	}

	if _, err := downstream.Fetch("origin"); err != nil {
		t.Fatal(err)
	}

	// Determine the default branch from upstream
	branchOut, _ := upstream.run("branch", "--show-current")
	branch := strings.TrimSpace(branchOut)
	if branch == "" {
		branch = "main" // fallback
	}

	// Checkout to create tracking branch locally
	if _, err := downstream.run("checkout", "-b", branch, "origin/"+branch); err != nil {
		t.Fatalf("Failed to checkout tracking branch: %v", err)
	}

	// 1. "Already up to date" check
	stats, out, err := downstream.Pull("origin", branch)
	if err != nil {
		t.Fatalf("Pull failed: %v\nOutput: %s", err, out)
	}
	if !stats.UpToDate {
		t.Errorf("Expected UpToDate=true, got %v", stats.UpToDate)
	}

	// 2. New commit in upstream
	file2 := filepath.Join(upstreamDir, "file2.txt")
	os.WriteFile(file2, []byte("content2"), 0644)
	if err := upstream.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Commit("second commit"); err != nil {
		t.Fatal(err)
	}

	// Pull should fetch 1 commit, 1 file changed
	stats, out, err = downstream.Pull("origin", branch)
	if err != nil {
		t.Fatalf("Pull failed: %v\nOutput: %s", err, out)
	}
	if stats.UpToDate {
		t.Errorf("Expected UpToDate=false, got true")
	}
	if stats.Commits != 1 {
		t.Errorf("Expected 1 commit, got %d", stats.Commits)
	}
	if stats.FilesChanged != 1 {
		t.Errorf("Expected 1 file changed, got %d", stats.FilesChanged)
	}
}

// TestPush covers Push and PushWithUpstream's reported stats against a bare
// local remote: an initial push and a push after new commits report
// NoOp=false with the right Commits count, while re-pushing an unchanged
// branch reports NoOp=true with zero commits.
func TestPush(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a bare remote repository
	remoteDir := filepath.Join(tmpDir, "remote.git")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatal(err)
	}
	remoteGit := New(remoteDir)
	if _, err := remoteGit.run("init", "--bare"); err != nil {
		t.Fatal(err)
	}

	// 2. Create a local repository
	localDir := filepath.Join(tmpDir, "local")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	g := New(localDir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("branch", "-M", "main"); err != nil {
		t.Fatal(err)
	}

	// 3. Add remote
	if err := g.RemoteAdd("origin", remoteDir); err != nil {
		t.Fatal(err)
	}

	// 4. Initial commit and push
	f := filepath.Join(localDir, "test.txt")
	if err := os.WriteFile(f, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("initial"); err != nil {
		t.Fatal(err)
	}

	// This is one ordered scenario (initial push -> no-op -> new commit), not
	// independent subtests: each step depends on the prior push state. Kept
	// linear so a filtered `-run` of a single step can't skip the setup.

	// Initial push creates the upstream branch: NoOp=false, 1 commit.
	stats, out, err := g.PushWithUpstream("origin", "main")
	if err != nil {
		t.Fatalf("PushWithUpstream failed: %v\nOutput: %s", err, out)
	}
	if stats.NoOp {
		t.Error("Expected NoOp to be false on initial push")
	}
	if stats.Commits != 1 {
		t.Errorf("Expected 1 commit, got %d", stats.Commits)
	}
	// Note: local pushes without transfer may emit empty stderr or varying
	// formats in newer git versions, so we don't assert stats.Objects here.

	// Re-pushing the unchanged branch is a no-op: NoOp=true, 0 commits.
	stats, out, err = g.Push("origin", "main")
	if err != nil {
		t.Fatalf("Push failed: %v\nOutput: %s", err, out)
	}
	if !stats.NoOp {
		t.Error("Expected NoOp to be true on unchanged push")
	}
	if stats.Commits != 0 {
		t.Errorf("Expected 0 commits on no-op, got %d", stats.Commits)
	}

	// After a new commit, push reports NoOp=false with 1 commit.
	if err := os.WriteFile(f, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("update"); err != nil {
		t.Fatal(err)
	}
	stats, out, err = g.Push("origin", "main")
	if err != nil {
		t.Fatalf("Push failed: %v\nOutput: %s", err, out)
	}
	if stats.NoOp {
		t.Error("Expected NoOp to be false on push with changes")
	}
	if stats.Commits != 1 {
		t.Errorf("Expected 1 commit, got %d", stats.Commits)
	}
}

// TestAddAll_ForceStagesIgnoredManifest guards that AddAll stages manifest.json
// even when a gitignore rule would exclude it. A user's global core.excludesFile
// matching e.g. *.json once silently dropped the manifest from the pushed repo,
// leaving clones with un-decryptable blobs and no relPath->hash mapping (issue #94).
func TestAddAll_ForceStagesIgnoredManifest(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	// Stand in for a hostile global core.excludesFile without touching the
	// user's real git config: .git/info/exclude has the same precedence.
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("manifest.json\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := g.AddAll(); err != nil {
		t.Fatalf("AddAll() error: %v", err)
	}

	out, err := g.run("ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "manifest.json") {
		t.Errorf("manifest.json was not staged despite ignore rule; git ls-files = %q", out)
	}
}

// TestAddAll_ForceStagesIgnoredObjects guards that AddAll stages the objects/
// blobs even when a gitignore rule would exclude them. A manifest force-staged
// over un-pushed blobs is worse than no manifest — unseal starts, then fails
// mid-restore on a missing object (issue #94 follow-up).
func TestAddAll_ForceStagesIgnoredObjects(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	// A hostile global rule matching the encrypted blobs (.age extension),
	// simulated via .git/info/exclude which shares core.excludesFile precedence.
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("*.age\n"), 0644); err != nil {
		t.Fatal(err)
	}

	objPath := filepath.Join(dir, "objects", "ab", "cdef0123.age")
	if err := os.MkdirAll(filepath.Dir(objPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objPath, []byte("ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := g.AddAll(); err != nil {
		t.Fatalf("AddAll() error: %v", err)
	}

	out, err := g.run("ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "objects/ab/cdef0123.age") {
		t.Errorf("object blob was not staged despite ignore rule; git ls-files = %q", out)
	}
}

// TestAddAll_ForceStagesIgnoredSealToml guards that AddAll stages seal.toml
// even when a gitignore rule (e.g. a global *.toml) would exclude it. Without
// the config a clone fails at config.Load before unseal can even look for the
// manifest.
func TestAddAll_ForceStagesIgnoredSealToml(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	// Stand in for a hostile global core.excludesFile without touching the
	// user's real git config: .git/info/exclude has the same precedence.
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("*.toml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "seal.toml"), []byte("config_version = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := g.AddAll(); err != nil {
		t.Fatalf("AddAll() error: %v", err)
	}

	out, err := g.run("ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "seal.toml") {
		t.Errorf("seal.toml was not staged despite ignore rule; git ls-files = %q", out)
	}
}

// TestRemoteOps_RejectExtTransportFromPoisonedConfig closes the config-
// resolution RCE vector: a remote whose URL is git's command-executing ext::
// transport, written straight into .git/config (bypassing the guarded
// RemoteAdd), must not run its payload when git resolves it by name during
// fetch/pull/push. The argument-level rejectUnsafeRemoteURL guard can't catch
// this — callers pass the remote name "origin", not the URL.
//
// Modern git already refuses ext:: by default, so the test sets
// protocol.ext.allow=user in the repo config to recreate the vulnerable
// condition (an older git, or a user who opted into ext:: globally). The fix
// must pin protocol.ext.allow=never on git's command line, which overrides
// that config; each op asserts the sentinel file the payload would touch
// never appears.
func TestRemoteOps_RejectExtTransportFromPoisonedConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   func(g *Git) error
	}{
		{"Fetch", func(g *Git) error { _, err := g.Fetch("origin"); return err }},
		{"Pull", func(g *Git) error { _, _, err := g.Pull("origin", "main"); return err }},
		{"Push", func(g *Git) error { _, _, err := g.Push("origin", "main"); return err }},
		{"PushWithUpstream", func(g *Git) error { _, _, err := g.PushWithUpstream("origin", "main"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			g := New(dir)
			if err := g.Init(); err != nil {
				t.Fatal(err)
			}
			if _, err := g.run("config", "user.name", "Test User"); err != nil {
				t.Fatal(err)
			}
			if _, err := g.run("config", "user.email", "test@example.com"); err != nil {
				t.Fatal(err)
			}
			// Recreate the vulnerable ambient policy the fix must override.
			if _, err := g.run("config", "protocol.ext.allow", "user"); err != nil {
				t.Fatal(err)
			}
			// A commit on a branch named main so push/pull have a real ref and
			// reach the transport (where the ext payload would fire) rather than
			// bailing earlier on a missing ref.
			if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := g.AddAll(); err != nil {
				t.Fatal(err)
			}
			if err := g.Commit("init"); err != nil {
				t.Fatal(err)
			}
			if _, err := g.run("branch", "-M", "main"); err != nil {
				t.Fatal(err)
			}

			// The payload: a self-contained executable the ext:: transport runs
			// on connect. Pointing ext:: at one no-arg script sidesteps the
			// transport's space-splitting of inline commands.
			pwned := filepath.Join(dir, "PWNED")
			helper := filepath.Join(dir, "helper.sh")
			if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch "+pwned+"\n"), 0755); err != nil {
				t.Fatal(err)
			}

			// Poison the config directly, as a hostile synced store would —
			// this never passes through RemoteAdd's rejectUnsafeRemoteURL guard.
			if _, err := g.run("config", "remote.origin.url", "ext::"+helper); err != nil {
				t.Fatal(err)
			}

			// The op is expected to error (transport refused, or later ref
			// negotiation fails); the security property under test is only that
			// the payload never ran, so we don't assert on the error itself.
			_ = tc.op(g)

			if _, err := os.Stat(pwned); err == nil {
				t.Fatalf("%s executed the ext:: payload (sentinel %s exists); protocol.ext.allow=never not applied", tc.name, pwned)
			}
		})
	}
}
