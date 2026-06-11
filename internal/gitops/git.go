package gitops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	gitPath     string
	gitPathOnce sync.Once
)

func getGitPath() string {
	gitPathOnce.Do(func() {
		path, err := exec.LookPath("git")
		if err == nil {
			gitPath = path
		} else {
			gitPath = "git"
		}
	})
	return gitPath
}

// Git wraps git CLI operations for a repository.
type Git struct {
	dir string
}

// PullStats summarizes a pull's effect on the local branch.
type PullStats struct {
	Commits      int
	FilesChanged int
	UpToDate     bool
	Elapsed      time.Duration
}

// PushStats summarizes a push's transfer to the remote.
type PushStats struct {
	Commits int
	Objects int
	Bytes   int64
	NoOp    bool
	Elapsed time.Duration
}

// New creates a Git instance for the given repo directory.
func New(repoDir string) *Git {
	return &Git{dir: repoDir}
}

// run executes a git command and returns combined output. Use this for
// user-facing diagnostics where interleaved stdout/stderr is acceptable.
func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command(getGitPath(), append([]string{"-C", g.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runSeparate executes a git command with stdout/stderr split, ANSI color
// suppressed so output is parseable regardless of user `color.ui` config.
// Use this for any path that parses git's output.
func (g *Git) runSeparate(args ...string) (stdout, stderr string, err error) {
	full := append([]string{"-C", g.dir, "-c", "color.ui=never"}, args...)
	cmd := exec.Command(getGitPath(), full...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), err
}

// Init initializes a new git repository.
func (g *Git) Init() error {
	_, err := g.run("init")
	return err
}

// Add stages files.
func (g *Git) Add(paths ...string) error {
	_, err := g.run(append([]string{"add", "--"}, paths...)...)
	return err
}

// AddAll stages all changes, then force-stages the seal store payload —
// manifest.json, the objects/ blobs, and seal.toml. A user's global gitignore
// (core.excludesFile) can otherwise silently drop files an unseal needs:
// without the manifest there is no relPath->hash mapping, without the objects
// the manifest's hashes point at blobs that were never committed, and without
// the config a clone fails before it even looks for the manifest.
func (g *Git) AddAll() error {
	if _, err := g.run("add", "."); err != nil {
		return err
	}
	for _, p := range []string{"manifest.json", "objects", "seal.toml"} {
		if _, err := os.Stat(filepath.Join(g.dir, p)); err != nil {
			continue
		}
		if _, err := g.run("add", "-f", "--", p); err != nil {
			return err
		}
	}
	return nil
}

// Commit creates a commit with the given message.
func (g *Git) Commit(msg string) error {
	_, err := g.run("commit", "-m", msg)
	return err
}

// HasCachedChanges checks if there are staged changes for the given paths.
func (g *Git) HasCachedChanges(paths ...string) bool {
	args := append([]string{"diff", "--quiet", "--cached", "--"}, paths...)
	_, _, err := g.runSeparate(args...)
	return err != nil
}

// CommitOnly creates a commit for only the specified files.
func (g *Git) CommitOnly(msg string, paths ...string) (string, error) {
	args := append([]string{"commit", "--only", "-m", msg, "--"}, paths...)
	return g.run(args...)
}

// HasChanges returns true if there are staged or unstaged changes.
func (g *Git) HasChanges() bool {
	out, _ := g.run("status", "--porcelain")
	return out != ""
}

// Push pushes to the given remote and branch and returns transfer stats
// alongside the human-readable combined output.
func (g *Git) Push(remote, branch string) (PushStats, string, error) {
	return g.pushWithArgs(remote, branch, false)
}

// PushWithUpstream pushes, sets upstream tracking, and returns transfer stats.
func (g *Git) PushWithUpstream(remote, branch string) (PushStats, string, error) {
	return g.pushWithArgs(remote, branch, true)
}

// pushWithArgs runs `git push --porcelain` (optionally with -u) and rolls
// the porcelain stdout + transfer-progress stderr into PushStats. The
// returned string is the combined output the caller should surface on
// error or in verbose mode.
func (g *Git) pushWithArgs(remote, branch string, setUpstream bool) (PushStats, string, error) {
	start := time.Now()
	stats := PushStats{}

	// Count commits the remote is missing before the push. With no upstream
	// yet the symbolic ref @{u} is undefined, so we count from the empty tree.
	commits := 0
	if g.HasUpstream() {
		if out, _, err := g.runSeparate("rev-list", "--count", "@{u}..HEAD"); err == nil {
			commits, _ = strconv.Atoi(strings.TrimSpace(out))
		}
	} else {
		if out, _, err := g.runSeparate("rev-list", "--count", "HEAD"); err == nil {
			commits, _ = strconv.Atoi(strings.TrimSpace(out))
		}
	}
	stats.Commits = commits

	args := []string{"push", "--porcelain"}
	if setUpstream {
		args = append(args, "-u")
	}
	// -- keeps a dash-prefixed remote/branch from being parsed as an option.
	args = append(args, "--", remote, branch)

	stdout, stderr, err := g.runSeparate(args...)
	combined := joinStreams(stdout, stderr)
	stats.NoOp = parsePorcelainNoOp(stdout)
	stats.Objects, stats.Bytes = parsePushTransfer(stderr)
	if stats.NoOp {
		stats.Commits = 0
	}
	stats.Elapsed = time.Since(start)
	return stats, combined, err
}

// Fetch fetches from the given remote.
func (g *Git) Fetch(remote string) (string, error) {
	return g.run("fetch", "--", remote)
}

// Pull pulls from the given remote and branch and returns a PullStats
// summary plus the combined output (which the merge driver parser also
// consumes for [enclaude-merge] lines on stderr).
func (g *Git) Pull(remote, branch string) (PullStats, string, error) {
	start := time.Now()
	stats := PullStats{}

	oldHead, _, _ := g.runSeparate("rev-parse", "HEAD")
	oldHead = strings.TrimSpace(oldHead)

	// We keep the legacy `run` (CombinedOutput) here for two reasons:
	// callers rely on the "CONFLICT" / "Already up to date" substrings in
	// the human-friendly form, and the merge driver's stderr arrives
	// interleaved with stdout — both ends up in the same string for the
	// parser. Color is suppressed via -c flags. The -- separator keeps a
	// dash-prefixed remote/branch from being parsed as a git option.
	cmd := exec.Command(getGitPath(), append([]string{"-C", g.dir, "-c", "color.ui=never", "pull", "--"}, remote, branch)...)
	rawOut, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(rawOut))

	if strings.Contains(out, "Already up to date") {
		stats.UpToDate = true
		stats.Elapsed = time.Since(start)
		return stats, out, err
	}

	if oldHead != "" {
		if cnt, _, e := g.runSeparate("rev-list", "--count", oldHead+"..HEAD"); e == nil {
			stats.Commits, _ = strconv.Atoi(strings.TrimSpace(cnt))
		}
		if stats.Commits > 0 {
			if diffOut, _, e := g.runSeparate("diff", "--shortstat", oldHead, "HEAD"); e == nil {
				stats.FilesChanged = parseShortstatFiles(diffOut)
			}
		}
	}
	stats.Elapsed = time.Since(start)
	return stats, out, err
}

// Merge merges the given ref into the current branch.
func (g *Git) Merge(ref string) (string, error) {
	return g.run("merge", "--", ref)
}

// MergeAbort aborts a merge in progress.
func (g *Git) MergeAbort() error {
	_, err := g.run("merge", "--abort")
	return err
}

// rejectUnsafeRemoteURL blocks remote URLs whose transport executes an
// arbitrary command. ext:: runs its argument as a shell command on every
// fetch/push, so it is a code-execution vector regardless of how safely
// the URL is passed as a positional — the -- separator cannot neutralize
// a valid-but-malicious positional. Called from every path that records a
// remote URL (add and set-url) so the guard can't be bypassed by editing.
func rejectUnsafeRemoteURL(url string) error {
	if strings.HasPrefix(url, "ext::") {
		return fmt.Errorf("refusing remote with ext:: URL (arbitrary command execution transport): %s", url)
	}
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("refusing remote URL starting with dash (flag injection risk): %s", url)
	}
	return nil
}

// rejectUnsafeRemoteName blocks remote names that start with a dash to
// prevent flag injection vulnerabilities when passed to git commands.
func rejectUnsafeRemoteName(name string) error {
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("refusing remote name starting with dash (flag injection risk): %s", name)
	}
	return nil
}

// RemoteAdd adds a git remote.
func (g *Git) RemoteAdd(name, url string) error {
	if err := rejectUnsafeRemoteName(name); err != nil {
		return err
	}
	if err := rejectUnsafeRemoteURL(url); err != nil {
		return err
	}
	_, err := g.run("remote", "add", "--", name, url)
	return err
}

// RemoteList returns the list of configured remotes.
func (g *Git) RemoteList() (string, error) {
	return g.run("remote", "-v")
}

// RemoteRemove removes a git remote.
func (g *Git) RemoteRemove(name string) error {
	if err := rejectUnsafeRemoteName(name); err != nil {
		return err
	}
	_, err := g.run("remote", "remove", "--", name)
	return err
}

// RemoteSetURL updates the URL of an existing git remote.
func (g *Git) RemoteSetURL(name, url string) error {
	if err := rejectUnsafeRemoteName(name); err != nil {
		return err
	}
	if err := rejectUnsafeRemoteURL(url); err != nil {
		return err
	}
	_, err := g.run("remote", "set-url", "--", name, url)
	return err
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
func (g *Git) ConfigMergeDriver(name string, driverArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// Note: os.Executable() on Linux can return a path under /proc/self/exe if unlinked
	// Note: POSIX single-quote escape won't work natively on Windows cmd.exe without bash
	var sb strings.Builder
	sb.WriteString("'" + strings.ReplaceAll(exe, "'", "'\\''") + "'")
	for _, arg := range driverArgs {
		sb.WriteString(" ")
		// Git special tokens should not be quoted so they are expanded by git
		if arg == "%O" || arg == "%A" || arg == "%B" || arg == "%L" || arg == "%P" {
			sb.WriteString(arg)
		} else {
			sb.WriteString("'" + strings.ReplaceAll(arg, "'", "'\\''") + "'")
		}
	}
	driverCmd := sb.String()

	if _, err := g.run("config", fmt.Sprintf("merge.%s.name", name), "Claude Seal "+name+" merge"); err != nil {
		return err
	}
	_, err = g.run("config", fmt.Sprintf("merge.%s.driver", name), driverCmd)
	return err
}

// HasRemote checks if a remote with the given name exists.
func (g *Git) HasRemote(name string) bool {
	out, err := g.run("remote")
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(out, "\n") {
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

// joinStreams concatenates stdout and stderr into the user-facing combined
// output. Empty streams are skipped so callers don't see leading/trailing
// blank lines when only one side produced anything.
func joinStreams(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

// parsePorcelainNoOp returns true if `git push --porcelain` reports that
// every ref was already up-to-date. Porcelain output starts with one-char
// flag tokens; `=` signals "up-to-date".
func parsePorcelainNoOp(stdout string) bool {
	if stdout == "" {
		return false
	}
	for line := range strings.SplitSeq(stdout, "\n") {
		line = strings.TrimSpace(line)
		// Skip header ("To <url>") and trailer ("Done").
		if line == "" || line == "Done" || strings.HasPrefix(line, "To ") {
			continue
		}
		if !strings.HasPrefix(line, "=") {
			return false
		}
	}
	return true
}

// pushTotalRE matches the "Total %d (delta ...)" line git emits on push.
var pushTotalRE = regexp.MustCompile(`Total (\d+)`)

// pushBytesRE matches the human bytes counter inside the progress line,
// e.g. "Writing objects: 100% (9/9), 412.00 KiB | ...".
var pushBytesRE = regexp.MustCompile(`([\d.]+)\s*(B|KiB|MiB|GiB)\b`)

// parsePushTransfer extracts object count and transferred-bytes estimate
// from the push command's stderr. Both fields are best-effort — git's
// progress wording varies by version and may be absent for empty pushes.
func parsePushTransfer(stderr string) (objects int, bts int64) {
	if m := pushTotalRE.FindStringSubmatch(stderr); len(m) == 2 {
		objects, _ = strconv.Atoi(m[1])
	}
	if m := pushBytesRE.FindStringSubmatch(stderr); len(m) == 3 {
		f, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			switch m[2] {
			case "B":
				bts = int64(f)
			case "KiB":
				bts = int64(f * 1024)
			case "MiB":
				bts = int64(f * 1024 * 1024)
			case "GiB":
				bts = int64(f * 1024 * 1024 * 1024)
			}
		}
	}
	return objects, bts
}

// shortstatFilesRE matches the leading "N file(s) changed" segment of
// `git diff --shortstat`. We avoid the localized word "file(s) changed"
// and rely on the leading digit run.
var shortstatFilesRE = regexp.MustCompile(`^\s*(\d+)\s+file`)

// parseShortstatFiles returns the file count from `git diff --shortstat`
// output, or 0 if the format wasn't recognized.
func parseShortstatFiles(s string) int {
	if m := shortstatFilesRE.FindStringSubmatch(s); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}
