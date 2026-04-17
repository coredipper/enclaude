package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReadmeRegenCommitDoesNotCaptureOtherStagedFiles(t *testing.T) {
	repoDir := t.TempDir()
	readmePath := filepath.Join(repoDir, "README.md")
	notesPath := filepath.Join(repoDir, "notes.txt")

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "config", "user.email", "test@example.com")

	writeTestFile(t, readmePath, "old readme\n")
	writeTestFile(t, notesPath, "tracked notes\n")
	runGit(t, repoDir, "add", "README.md", "notes.txt")
	runGit(t, repoDir, "commit", "-m", "initial")

	writeTestFile(t, readmePath, "new readme\n")
	writeTestFile(t, notesPath, "manually staged change\n")
	runGit(t, repoDir, "add", "notes.txt")

	cmd := &cobra.Command{}
	committed, err := stageAndCommitReadme(cmd, repoDir)
	if err != nil {
		t.Fatalf("stageAndCommitReadme() error = %v", err)
	}
	if !committed {
		t.Fatal("expected README.md change to be committed")
	}

	committedFiles := strings.Fields(runGit(t, repoDir, "show", "--pretty=", "--name-only", "HEAD"))
	if len(committedFiles) != 1 || committedFiles[0] != "README.md" {
		t.Fatalf("expected README-only commit, got %v", committedFiles)
	}

	stagedFiles := strings.Fields(runGit(t, repoDir, "diff", "--cached", "--name-only"))
	if len(stagedFiles) != 1 || stagedFiles[0] != "notes.txt" {
		t.Fatalf("expected notes.txt to remain staged, got %v", stagedFiles)
	}

	if got := runGit(t, repoDir, "show", "HEAD:README.md"); got != "new readme\n" {
		t.Fatalf("expected committed README contents to match, got %q", got)
	}
	if got := runGit(t, repoDir, "show", "HEAD:notes.txt"); got != "tracked notes\n" {
		t.Fatalf("expected unrelated file to stay out of commit, got %q", got)
	}
}
func runGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
