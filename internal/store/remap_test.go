package store

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestEncodePath_DotAndSlash pins the project-key encoding that Claude Code
// uses: BOTH '/' and '.' collapse to '-'. The dot rule is the subtle one — a
// hidden dir like .claude-worktrees yields a doubled dash where the parent
// separator and the leading dot meet, and getting it wrong would make the
// home-prefix swap miss worktree project dirs.
func TestEncodePath_DotAndSlash(t *testing.T) {
	cases := []struct{ abs, want string }{
		{"/Users/bob/core/enclaude", "-Users-bob-core-enclaude"},
		{"/home/daniel", "-home-daniel"},
		{"/Users/bob/core/enclaude/.claude-worktrees/x", "-Users-bob-core-enclaude--claude-worktrees-x"},
		{"/Users/bob/v1.2/app", "-Users-bob-v1-2-app"},
	}
	for _, c := range cases {
		if got := encodePath(c.abs); got != c.want {
			t.Errorf("encodePath(%q) = %q, want %q", c.abs, got, c.want)
		}
	}
}

// TestRemapKey_HomePrefixSwap covers the decode-free core transform: a leading
// encode(srcHome) segment of a projects/ key is swapped for encode(dstHome),
// matched only at an encoded-segment boundary so "-Users-bob" can't capture
// "-Users-bobby". Non-projects/ keys and project paths outside srcHome are
// returned untouched (ok=false).
func TestRemapKey_HomePrefixSwap(t *testing.T) {
	const src, dst = "-home-daniel", "-Users-bob"
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"basic file", "projects/-home-daniel-core-enclaude/a.jsonl", "projects/-Users-bob-core-enclaude/a.jsonl", true},
		{"in-project remainder preserved", "projects/-home-daniel-core-enclaude/memory/MEMORY.md", "projects/-Users-bob-core-enclaude/memory/MEMORY.md", true},
		{"worktree doubled dash", "projects/-home-daniel-core-enclaude--claude-worktrees-a/x.jsonl", "projects/-Users-bob-core-enclaude--claude-worktrees-a/x.jsonl", true},
		{"exact segment", "projects/-home-daniel", "projects/-Users-bob", true},
		{"non-projects untouched", "history.jsonl", "history.jsonl", false},
		{"settings untouched", "settings.json", "settings.json", false},
		{"different home untouched", "projects/-srv-work-x/y.jsonl", "projects/-srv-work-x/y.jsonl", false},
	}
	for _, c := range cases {
		got, ok := RemapKey(c.in, src, dst)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: RemapKey(%q) = (%q,%v), want (%q,%v)", c.name, c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestRemapKey_BoundaryGuard guards the segment-boundary match: a srcHome that
// is a string prefix of a longer user name must not match. -Users-bob must not
// capture -Users-bobby.
func TestRemapKey_BoundaryGuard(t *testing.T) {
	got, ok := RemapKey("projects/-Users-bobby-proj/x.jsonl", "-Users-bob", "-home-z")
	if ok {
		t.Errorf("RemapKey matched across a segment boundary: got %q", got)
	}
}

// TestPlanRemap_GroupsAndProposes verifies PlanRemap produces one plan per
// foreign project dir (not per file), proposes the home-prefix swap to the
// local home, and leaves already-local and non-home project dirs alone.
func TestPlanRemap_GroupsAndProposes(t *testing.T) {
	m := NewManifest("dev")
	for _, k := range []string{
		"projects/-home-daniel-core-enclaude/a.jsonl",
		"projects/-home-daniel-core-enclaude/b.jsonl",
		"projects/-home-daniel-other/c.jsonl",
		"projects/-Users-bob-local/d.jsonl", // already local
		"history.jsonl",                     // non-project
	} {
		m.Files[k] = FileEntry{ContentHash: "h"}
	}

	plans := PlanRemap(m, "-Users-bob", "", nil)
	sort.Slice(plans, func(i, j int) bool { return plans[i].SrcKey < plans[j].SrcKey })

	want := []RemapPlan{
		{SrcKey: "-home-daniel-core-enclaude", DstKey: "-Users-bob-core-enclaude", Accepted: true},
		{SrcKey: "-home-daniel-other", DstKey: "-Users-bob-other", Accepted: true},
	}
	if !reflect.DeepEqual(plans, want) {
		t.Errorf("PlanRemap plans = %+v, want %+v", plans, want)
	}
}

// TestPlanRemap_OverrideWins verifies a device-local override replaces the
// heuristic proposal for its source key and is marked accepted.
func TestPlanRemap_OverrideWins(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-home-daniel-other/c.jsonl"] = FileEntry{ContentHash: "h"}

	overrides := map[string]string{"-home-daniel-other": "-Users-bob-CUSTOM"}
	plans := PlanRemap(m, "-Users-bob", "", overrides)
	if len(plans) != 1 || plans[0].DstKey != "-Users-bob-CUSTOM" || !plans[0].Accepted {
		t.Fatalf("override not applied: %+v", plans)
	}
}

// TestPlanRemap_DashedLocalHomeNotRemapped guards against misclassifying a
// LOCAL project as foreign when the home's username contains a '-'
// (e.g. /Users/bob-smith → -Users-bob-smith). The lossy encodedHomePrefix
// heuristic alone would read "-Users-bob" as the home and rewrite the local
// key, after which the delete pass could remove real local files. The exact
// dstHomeEnc boundary check must take precedence and leave it untouched.
func TestPlanRemap_DashedLocalHomeNotRemapped(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-Users-bob-smith-myproj/a.jsonl"] = FileEntry{ContentHash: "h"}
	if plans := PlanRemap(m, "-Users-bob-smith", "", nil); len(plans) != 0 {
		t.Errorf("local dashed-home project must not be remapped, got %+v", plans)
	}
}

// TestPlanRemap_OriginHomeAuthoritative verifies the authoritative origin-home
// prefix is preferred over the heuristic, so a foreign home with a dashed
// username (-home-dan-lee) remaps as one unit instead of being mis-split into
// -home-dan by encodedHomePrefix.
func TestPlanRemap_OriginHomeAuthoritative(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-home-dan-lee-proj/a.jsonl"] = FileEntry{ContentHash: "h"}
	plans := PlanRemap(m, "-Users-bob", "-home-dan-lee", nil)
	if len(plans) != 1 || plans[0].DstKey != "-Users-bob-proj" {
		t.Fatalf("origin-home swap wrong (heuristic mis-split?): %+v", plans)
	}
}

// TestPlanRemap_LocalHomeIsPrefixOfOrigin guards the ambiguous case where the
// local home is a boundary prefix of the authoritative origin home
// (dst=/Users/bob, origin=/Users/bob-smith). The foreign key
// -Users-bob-smith-core must remap via the origin home to -Users-bob-core
// rather than be skipped as "already local" — otherwise unseal restores the
// foreign key and the delete pass can remove the real local -Users-bob-* files.
func TestPlanRemap_LocalHomeIsPrefixOfOrigin(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-Users-bob-smith-core/a.jsonl"] = FileEntry{ContentHash: "h"}
	plans := PlanRemap(m, "-Users-bob", "-Users-bob-smith", nil)
	if len(plans) != 1 || plans[0].DstKey != "-Users-bob-core" {
		t.Fatalf("expected remap to -Users-bob-core via authoritative origin home, got %+v", plans)
	}
}

// TestApplyRemap_EffectiveManifestKeys verifies ApplyRemap returns a new
// manifest whose accepted-plan keys are rewritten while FileEntry values and
// untouched keys are preserved.
func TestApplyRemap_EffectiveManifestKeys(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-home-daniel-core-enclaude/a.jsonl"] = FileEntry{ContentHash: "ha", JSONLLineCount: 3}
	m.Files["projects/-Users-bob-local/d.jsonl"] = FileEntry{ContentHash: "hd"}
	m.Files["history.jsonl"] = FileEntry{ContentHash: "hh"}

	plans := []RemapPlan{{SrcKey: "-home-daniel-core-enclaude", DstKey: "-Users-bob-core-enclaude", Accepted: true}}
	out := ApplyRemap(m, plans)

	if _, gone := out.Files["projects/-home-daniel-core-enclaude/a.jsonl"]; gone {
		t.Error("foreign key still present after remap")
	}
	e, ok := out.Files["projects/-Users-bob-core-enclaude/a.jsonl"]
	if !ok || e.ContentHash != "ha" || e.JSONLLineCount != 3 {
		t.Errorf("remapped entry missing or altered: %+v ok=%v", e, ok)
	}
	if _, ok := out.Files["projects/-Users-bob-local/d.jsonl"]; !ok {
		t.Error("local key should be untouched")
	}
	if _, ok := out.Files["history.jsonl"]; !ok {
		t.Error("non-project key should be untouched")
	}
	// Input manifest must not be mutated.
	if _, ok := m.Files["projects/-home-daniel-core-enclaude/a.jsonl"]; !ok {
		t.Error("ApplyRemap mutated the input manifest")
	}
}

// TestApplyRemap_CollisionKeepsHigherLineCount verifies that when a remapped
// key collides with an existing one (same logical project synced from two
// machines), the entry with the larger JSONLLineCount wins — order-independent.
func TestApplyRemap_CollisionKeepsHigherLineCount(t *testing.T) {
	m := NewManifest("dev")
	m.Files["projects/-home-daniel-p/s.jsonl"] = FileEntry{ContentHash: "foreign", JSONLLineCount: 10}
	m.Files["projects/-Users-bob-p/s.jsonl"] = FileEntry{ContentHash: "local", JSONLLineCount: 5}

	plans := []RemapPlan{{SrcKey: "-home-daniel-p", DstKey: "-Users-bob-p", Accepted: true}}
	out := ApplyRemap(m, plans)

	e := out.Files["projects/-Users-bob-p/s.jsonl"]
	if e.ContentHash != "foreign" {
		t.Errorf("collision winner = %q, want foreign (higher line count)", e.ContentHash)
	}
}

// TestApplyRemap_TieKeepsLocalDeterministic guards collision resolution when a
// remapped key lands on an existing local key and both have equal (here zero)
// line counts — common for memory/.md and sessions-index/.json. The local entry
// must win deterministically rather than depending on map iteration order, so
// the loop runs many times to shake out nondeterminism.
func TestApplyRemap_TieKeepsLocalDeterministic(t *testing.T) {
	for i := range 50 {
		m := NewManifest("dev")
		m.Files["projects/-home-daniel-p/memory/MEMORY.md"] = FileEntry{ContentHash: "foreign"}
		m.Files["projects/-Users-bob-p/memory/MEMORY.md"] = FileEntry{ContentHash: "local"}
		out := ApplyRemap(m, []RemapPlan{{SrcKey: "-home-daniel-p", DstKey: "-Users-bob-p", Accepted: true}})
		if got := out.Files["projects/-Users-bob-p/memory/MEMORY.md"].ContentHash; got != "local" {
			t.Fatalf("tie should keep the local entry deterministically, got %q (iter %d)", got, i)
		}
	}
}

// TestLoadSaveOverrides_RoundTrip verifies the device-local override map
// persists and reloads from <sealDir>/projectmap.local.toml.
func TestLoadSaveOverrides_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := LoadOverrides(dir); len(got) != 0 {
		t.Errorf("LoadOverrides on empty dir = %v, want empty", got)
	}
	in := map[string]string{
		"-home-daniel-core-enclaude": "-Users-bob-core-enclaude",
		"-srv-work-x":                "-Users-bob-work-x",
	}
	if err := SaveOverrides(dir, in); err != nil {
		t.Fatalf("SaveOverrides: %v", err)
	}
	got := LoadOverrides(dir)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round-trip = %v, want %v", got, in)
	}
	if _, err := filepath.Abs(filepath.Join(dir, "projectmap.local.toml")); err != nil {
		t.Fatal(err)
	}
}
