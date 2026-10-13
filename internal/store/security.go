package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// localStateFile returns a path for local, non-synced state. It lives under
// .git so it is never committed; the worktree fallback is only used when .git
// is unavailable and is covered by .gitignore.
func localStateFile(sealDir, name string) string {
	gitDir := filepath.Join(sealDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return filepath.Join(gitDir, name)
	}
	return filepath.Join(sealDir, ".enclaude-"+name)
}

func loadPathSet(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}

// managedFile is the local ledger of paths this device has sealed. Deletions
// are decided against this ledger rather than the synced include patterns, so a
// compromised remote cannot cause unseal to delete files that were never sealed.
func managedFile(sealDir string) string { return localStateFile(sealDir, "managed.json") }

func loadManaged(sealDir string) map[string]bool { return loadPathSet(managedFile(sealDir)) }

// saveManaged always writes the ledger, even when empty, so its existence marks
// that it has been initialized and must not be re-seeded from the manifest.
func saveManaged(sealDir string, managed map[string]bool) error {
	paths := make([]string, 0, len(managed))
	for p := range managed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(managedFile(sealDir), data, 0600)
}

// trimEmptyParents removes now-empty parent directories up to (but not
// including) root. Best effort.
func trimEmptyParents(absPath, root string) {
	dir := filepath.Dir(absPath)
	for dir != root && len(dir) > len(root) && dir[:len(root)+1] == root+string(os.PathSeparator) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
