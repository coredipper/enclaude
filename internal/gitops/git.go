package gitops

import (
	"fmt"
	"os/exec"
	"strings"
)

// Git wraps git CLI operations for a repository.
type Git struct {
	dir string
}

// New creates a Git instance for the given repo directory.
func New(repoDir string) *Git {
	return &Git{dir: repoDir}
}

// run executes a git command and returns combined output.
func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Init initializes a new git repository.
func (g *Git) Init() error {
	_, err := g.run("init")
	return err
}

// Add stages files.
func (g *Git) Add(paths ...string) error {
	_, err := g.run(append([]string{"add"}, paths...)...)
	return err
}

// AddAll stages all changes.
func (g *Git) AddAll() error {
	_, err := g.run("add", ".")
	return err
}

// Commit creates a commit with the given message.
func (g *Git) Commit(msg string) error {
	_, err := g.run("commit", "-m", msg)
	return err
}

// HasChanges returns true if there are staged or unstaged changes.
func (g *Git) HasChanges() bool {
	out, _ := g.run("status", "--porcelain")
	return out != ""
}

// Push pushes to the given remote and branch.
func (g *Git) Push(remote, branch string) (string, error) {
	return g.run("push", remote, branch)
}

// PushWithUpstream pushes and sets upstream tracking.
func (g *Git) PushWithUpstream(remote, branch string) (string, error) {
	return g.run("push", "-u", remote, branch)
}

// Fetch fetches from the given remote.
func (g *Git) Fetch(remote string) (string, error) {
	return g.run("fetch", remote)
}

// Pull pulls from the given remote and branch.
func (g *Git) Pull(remote, branch string) (string, error) {
	return g.run("pull", remote, branch)
}

// Merge merges the given ref into the current branch.
func (g *Git) Merge(ref string) (string, error) {
	return g.run("merge", ref)
}

// MergeAbort aborts a merge in progress.
func (g *Git) MergeAbort() error {
	_, err := g.run("merge", "--abort")
	return err
}

// RemoteAdd adds a git remote.
func (g *Git) RemoteAdd(name, url string) error {
	_, err := g.run("remote", "add", name, url)
	return err
}

// RemoteList returns the list of configured remotes.
func (g *Git) RemoteList() (string, error) {
	return g.run("remote", "-v")
}

// CurrentBranch returns the current branch name.
func (g *Git) CurrentBranch() (string, error) {
	return g.run("rev-parse", "--abbrev-ref", "HEAD")
}

// Log returns the git log in oneline format.
func (g *Git) Log(n int) (string, error) {
	return g.run("log", fmt.Sprintf("-%d", n), "--oneline")
}

// LogFull returns detailed git log.
func (g *Git) LogFull(n int) (string, error) {
	return g.run("log", fmt.Sprintf("-%d", n), "--format=%h %s (%ar)")
}

// ConfigMergeDriver registers a custom merge driver.
func (g *Git) ConfigMergeDriver(name, driverCmd string) error {
	if _, err := g.run("config", fmt.Sprintf("merge.%s.name", name), "Claude Seal "+name+" merge"); err != nil {
		return err
	}
	_, err := g.run("config", fmt.Sprintf("merge.%s.driver", name), driverCmd)
	return err
}

// HasRemote checks if a remote with the given name exists.
func (g *Git) HasRemote(name string) bool {
	out, err := g.run("remote")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// HasUpstream checks if the current branch has an upstream configured.
func (g *Git) HasUpstream() bool {
	_, err := g.run("rev-parse", "--abbrev-ref", "@{u}")
	return err == nil
}

// ShowFileAtRef returns the contents of a file at a specific git ref.
func (g *Git) ShowFileAtRef(ref, path string) (string, error) {
	return g.run("show", ref+":"+path)
}

// Checkout restores files from a specific ref.
func (g *Git) Checkout(ref string, paths ...string) (string, error) {
	args := append([]string{"checkout", ref, "--"}, paths...)
	return g.run(args...)
}
