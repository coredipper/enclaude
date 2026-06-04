package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// Claude Code keys per-project state under projects/<encode(absProjectPath)>/…,
// where the directory name is the project's absolute path with both '/' and '.'
// replaced by '-'. That key embeds the originating machine's home, so a store
// synced to a second machine restores under a key the local Claude Code never
// queries. The functions here rewrite the project-dir key (only) so the project
// becomes discoverable locally. The absolute paths embedded *inside* transcripts
// are left as a historical record — Claude Code does not re-execute them.

// encodePath mirrors Claude Code's project-dir key derivation: both '/' and '.'
// collapse to '-'. The transform is lossy (a real '-' in a path is
// indistinguishable from an encoded separator), so it is deliberately never
// inverted — remapping is a prefix swap on the already-encoded form.
func encodePath(abs string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(abs)
}

// RemapMode controls how foreign project keys are handled on unseal.
type RemapMode int

const (
	// RemapOff restores verbatim (legacy behavior).
	RemapOff RemapMode = iota
	// RemapAuto accepts the deterministic home-prefix swaps without prompting.
	RemapAuto
	// RemapInteractive consults DefaultRemapResolver to let the user decide.
	RemapInteractive
)

// RemapResolver lets the cmd layer present proposed plans to the user and
// return the accepted/edited set. Consulted only in RemapInteractive mode; nil
// on a non-TTY. Mirrors the crypto.DefaultPassphraseFunc package-var seam so the
// store package stays free of any terminal dependency and tests can inject one.
type RemapResolver func([]RemapPlan) ([]RemapPlan, error)

// DefaultRemapResolver is wired by cmd to a ui-backed prompt.
var DefaultRemapResolver RemapResolver

// RemapPlan is the decision for one foreign project dir: every manifest key
// under projects/<SrcKey>/ is rewritten to the same subpath under projects/<DstKey>/.
type RemapPlan struct {
	SrcKey   string // encoded project segment, e.g. "-home-daniel-core-enclaude"
	DstKey   string // local segment, e.g. "-Users-bob-core-enclaude"
	Accepted bool
}

// homeDir returns the local machine's home directory: the parent of ~/.claude
// when claudeDir points there, else os.UserHomeDir(). After config.Load +
// getClaudeDir, cfg.Seal.ClaudeDir is the local ~/.claude, so its parent is the
// home where this machine's projects live.
func homeDir(claudeDir string) string {
	home := strings.TrimSuffix(claudeDir, string(filepath.Separator)+".claude")
	if home == "" || home == claudeDir {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
	}
	return home
}

// localHomeEnc returns the local machine's home directory in encoded form.
func localHomeEnc(cfg *config.Config) string {
	return encodePath(homeDir(cfg.Seal.ClaudeDir))
}

// encodedHomePrefix returns the leading encoded home-dir segment of an encoded
// project segment ("-Users-bob", "-home-daniel", "-root"), or "" if the segment
// doesn't start with a recognizable home.
func encodedHomePrefix(seg string) string {
	parts := strings.Split(seg, "-") // leading "-" yields an empty parts[0]
	if len(parts) < 2 {
		return ""
	}
	switch parts[1] {
	case "Users", "home":
		if len(parts) >= 3 && parts[2] != "" {
			return "-" + parts[1] + "-" + parts[2]
		}
	case "root":
		return "-root"
	}
	return ""
}

// swapEncodedPrefix replaces a leading oldEnc segment of s with newEnc, matched
// only at an encoded-segment boundary (exact, or followed by '-') so "-Users-bob"
// can't capture "-Users-bobby". Returns ok=false when s isn't under oldEnc.
func swapEncodedPrefix(s, oldEnc, newEnc string) (string, bool) {
	if s == oldEnc {
		return newEnc, true
	}
	if strings.HasPrefix(s, oldEnc+"-") {
		return newEnc + s[len(oldEnc):], true
	}
	return s, false
}

// splitProjectKey splits a manifest relPath of the form
// "projects/<seg>[/<sub>]" into its encoded project segment and the in-project
// remainder. ok=false for any key not under projects/.
func splitProjectKey(relPath string) (seg, sub string, ok bool) {
	rest, ok := strings.CutPrefix(relPath, "projects/")
	if !ok {
		return "", "", false
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i], rest[i:], true
	}
	return rest, "", true
}

// RemapKey swaps a leading encode(srcHome) segment of a projects/ relPath for
// encode(dstHome), preserving the in-project remainder. Returns (relPath,false)
// for non-projects/ keys and for project paths not under srcHome.
func RemapKey(relPath, srcHomeEnc, dstHomeEnc string) (string, bool) {
	seg, sub, ok := splitProjectKey(relPath)
	if !ok {
		return relPath, false
	}
	newSeg, ok := swapEncodedPrefix(seg, srcHomeEnc, dstHomeEnc)
	if !ok {
		return relPath, false
	}
	return "projects/" + newSeg + sub, true
}

// PlanRemap groups the manifest's project dirs by their encoded segment and, for
// each whose home prefix differs from dstHomeEnc, proposes a swap to dstHomeEnc.
// originHomeEnc (encode(manifest.OriginHome), may be "") is treated as an extra
// known source prefix so non-standard homes still match. A device-local override
// (srcKey→dstKey) wins over the heuristic. One plan per project dir, sorted by
// SrcKey for deterministic output. Already-local and unrecognized dirs get no
// plan (restored verbatim) unless an override names them.
func PlanRemap(m *Manifest, dstHomeEnc, originHomeEnc string, overrides map[string]string) []RemapPlan {
	seen := map[string]bool{}
	var plans []RemapPlan

	for relPath := range m.Files {
		seg, _, ok := splitProjectKey(relPath)
		if !ok || seg == "" || seen[seg] {
			continue
		}
		seen[seg] = true

		if dst, ok := overrides[seg]; ok {
			plans = append(plans, RemapPlan{SrcKey: seg, DstKey: dst, Accepted: true})
			continue
		}

		src := encodedHomePrefix(seg)
		if src == "" && originHomeEnc != "" {
			if _, ok := swapEncodedPrefix(seg, originHomeEnc, ""); ok {
				src = originHomeEnc
			}
		}
		if src == "" || src == dstHomeEnc {
			continue // unrecognized, or already local
		}
		if dst, ok := swapEncodedPrefix(seg, src, dstHomeEnc); ok {
			plans = append(plans, RemapPlan{SrcKey: seg, DstKey: dst, Accepted: true})
		}
	}

	sortPlans(plans)
	return plans
}

func sortPlans(plans []RemapPlan) {
	for i := 1; i < len(plans); i++ {
		for j := i; j > 0 && plans[j-1].SrcKey > plans[j].SrcKey; j-- {
			plans[j-1], plans[j] = plans[j], plans[j-1]
		}
	}
}

// ApplyRemap returns a NEW manifest whose keys under each accepted plan's SrcKey
// are rewritten to its DstKey; FileEntry values are shared (treated immutable)
// and untouched keys pass through. When a rewrite collides with an existing key
// (the same logical project synced from two machines), the entry with the larger
// JSONLLineCount wins — order-independent so the result is deterministic.
func ApplyRemap(m *Manifest, plans []RemapPlan) *Manifest {
	repl := make(map[string]string, len(plans))
	for _, p := range plans {
		if p.Accepted && p.DstKey != "" && p.DstKey != p.SrcKey {
			repl[p.SrcKey] = p.DstKey
		}
	}

	out := &Manifest{
		Version:    m.Version,
		DeviceID:   m.DeviceID,
		SealedAt:   m.SealedAt,
		OriginHome: m.OriginHome,
		Files:      make(map[string]FileEntry, len(m.Files)),
	}
	for relPath, entry := range m.Files {
		nk := relPath
		if seg, sub, ok := splitProjectKey(relPath); ok {
			if dst, mapped := repl[seg]; mapped {
				nk = "projects/" + dst + sub
			}
		}
		if existing, clash := out.Files[nk]; clash && entry.JSONLLineCount <= existing.JSONLLineCount {
			continue // keep the existing (higher or equal line count) entry
		}
		out.Files[nk] = entry
	}
	return out
}

// remapManifest rewrites foreign project keys in m into an effective local
// manifest per mode. Returns m unchanged for RemapOff or when nothing foreign
// is found. In RemapInteractive it consults DefaultRemapResolver and persists
// the resulting decisions as device-local overrides so later unseals are silent.
func remapManifest(m *Manifest, cfg *config.Config, mode RemapMode) (*Manifest, error) {
	if mode == RemapOff {
		return m, nil
	}
	dst := localHomeEnc(cfg)
	var originEnc string
	if m.OriginHome != "" {
		originEnc = encodePath(m.OriginHome)
	}
	plans := PlanRemap(m, dst, originEnc, LoadOverrides(cfg.Seal.SealDir))
	if len(plans) == 0 {
		return m, nil
	}
	if mode == RemapInteractive && DefaultRemapResolver != nil {
		resolved, err := DefaultRemapResolver(plans)
		if err != nil {
			return m, err
		}
		plans = resolved
		persistOverrides(cfg.Seal.SealDir, plans)
	}
	return ApplyRemap(m, plans), nil
}

// persistOverrides records the accepted plans as device-local overrides
// (best-effort: the remap is already applied, so a write failure only costs a
// re-prompt next time).
func persistOverrides(sealDir string, plans []RemapPlan) {
	ov := LoadOverrides(sealDir)
	changed := false
	for _, p := range plans {
		if p.Accepted && p.DstKey != "" && ov[p.SrcKey] != p.DstKey {
			ov[p.SrcKey] = p.DstKey
			changed = true
		}
	}
	if changed {
		_ = SaveOverrides(sealDir, ov)
	}
}

// ProjectInfo describes one synced project directory for `enclaude project list`.
type ProjectInfo struct {
	Key      string // encoded project segment
	Files    int    // manifest entries under it
	Foreign  bool   // its home prefix differs from this machine's
	Override string // device-local override target, "" if none
}

// ProjectDirs groups the manifest's project keys and classifies each as local
// or foreign relative to this machine, attaching any device-local override.
func ProjectDirs(m *Manifest, cfg *config.Config) []ProjectInfo {
	dst := localHomeEnc(cfg)
	overrides := LoadOverrides(cfg.Seal.SealDir)
	counts := map[string]int{}
	for relPath := range m.Files {
		if seg, _, ok := splitProjectKey(relPath); ok && seg != "" {
			counts[seg]++
		}
	}
	out := make([]ProjectInfo, 0, len(counts))
	for seg, n := range counts {
		prefix := encodedHomePrefix(seg)
		out = append(out, ProjectInfo{
			Key:      seg,
			Files:    n,
			Foreign:  prefix != "" && prefix != dst,
			Override: overrides[seg],
		})
	}
	return out
}

// NormalizeProjectKey accepts either an absolute project path or an
// already-encoded segment and returns the encoded segment, so `project map`
// can take whichever the user has on hand.
func NormalizeProjectKey(s string) string {
	if strings.Contains(s, "/") {
		return encodePath(s)
	}
	return s
}

// projectMap is the on-disk shape of the device-local override file.
type projectMap struct {
	Overrides map[string]string `toml:"overrides"`
}

func overridesPath(sealDir string) string {
	return filepath.Join(sealDir, "projectmap.local.toml")
}

// LoadOverrides reads the device-local project-key overrides. Missing file or
// parse error yields an empty map — overrides are a convenience, never required.
func LoadOverrides(sealDir string) map[string]string {
	data, err := os.ReadFile(overridesPath(sealDir))
	if err != nil {
		return map[string]string{}
	}
	var pm projectMap
	if err := toml.Unmarshal(data, &pm); err != nil || pm.Overrides == nil {
		return map[string]string{}
	}
	return pm.Overrides
}

// SaveOverrides writes the device-local project-key overrides. The file is
// machine-specific and must never be synced; init's .gitignore force-ignores it
// for new stores, and this also appends the ignore rule for older ones.
func SaveOverrides(sealDir string, overrides map[string]string) error {
	data, err := toml.Marshal(projectMap{Overrides: overrides})
	if err != nil {
		return fmt.Errorf("marshaling project map: %w", err)
	}
	if err := os.WriteFile(overridesPath(sealDir), data, 0600); err != nil {
		return err
	}
	return ensureOverridesIgnored(sealDir)
}

// ensureOverridesIgnored appends the override filename to .gitignore when a
// negation-style entry isn't already there, so a plain `git add .` can't commit
// this machine's mapping to the synced store.
func ensureOverridesIgnored(sealDir string) error {
	const rule = "projectmap.local.toml"
	path := filepath.Join(sealDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == rule {
			return nil
		}
	}
	body := string(data)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "# Device-local project-key map — never synced\n" + rule + "\n"
	return os.WriteFile(path, []byte(body), 0644)
}
