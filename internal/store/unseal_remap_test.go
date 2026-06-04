package store

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// sealForeignProject seals a single project transcript under a foreign home key
// (projects/-home-daniel-core-enclaude/a.jsonl) and returns the seal dir and the
// age identity. The encoded "-home-daniel" prefix stands in for a project sealed
// on a machine whose home differs from the one that later unseals.
func sealForeignProject(t *testing.T) (sealDir string, identity *age.X25519Identity, foreignRel string) {
	t.Helper()
	srcClaude := filepath.Join(t.TempDir(), ".claude")
	foreignRel = "projects/-home-daniel-core-enclaude/a.jsonl"
	full := filepath.Join(srcClaude, foreignRel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`{"type":"user"}`), 0644); err != nil {
		t.Fatal(err)
	}

	sealDir = t.TempDir()
	id, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cfg := config.DefaultConfig(srcClaude, sealDir)
	if _, err := Seal(cfg, id.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return sealDir, id, foreignRel
}

// TestUnseal_ForeignProjectKeyLandsLocalAndSurvivesDeletePass is the headline
// guard for the cross-device remap: a project sealed under a foreign home key
// must restore under THIS machine's key, and — critically — the manifest-is-
// source-of-truth delete pass must not nuke the freshly-restored file. The bug
// it pins: if remap touched only the restore loop, the delete reconciliation
// would scan the local-key file, miss it among the raw (foreign) manifest keys,
// and delete what was just written.
func TestUnseal_ForeignProjectKeyLandsLocalAndSurvivesDeletePass(t *testing.T) {
	sealDir, id, foreignRel := sealForeignProject(t)

	dstHome := t.TempDir()
	dstClaude := filepath.Join(dstHome, ".claude")
	cfg := config.DefaultConfig(dstClaude, sealDir)

	stats, err := Unseal(cfg, id, false, nil, WithRemap(RemapAuto))
	if err != nil {
		t.Fatalf("Unseal: %v", err)
	}

	localSeg := encodePath(dstHome) + "-core-enclaude"
	localPath := filepath.Join(dstClaude, "projects", localSeg, "a.jsonl")
	if _, err := os.Stat(localPath); err != nil {
		t.Errorf("restored file not under local key %s: %v", localPath, err)
	}
	if _, err := os.Stat(filepath.Join(dstClaude, foreignRel)); !os.IsNotExist(err) {
		t.Errorf("file should not exist under the foreign key %s", foreignRel)
	}
	if stats.Deleted != 0 {
		t.Errorf("delete pass removed %d file(s); the remapped file was wrongly reconciled away", stats.Deleted)
	}
	if stats.Restored == 0 {
		t.Error("expected at least one restored file")
	}
}

// TestUnseal_RemapOffPreservesLegacy verifies the opt-out: with RemapOff the
// file restores verbatim under the foreign key (the pre-feature behavior).
func TestUnseal_RemapOffPreservesLegacy(t *testing.T) {
	sealDir, id, foreignRel := sealForeignProject(t)

	dstClaude := filepath.Join(t.TempDir(), ".claude")
	cfg := config.DefaultConfig(dstClaude, sealDir)

	if _, err := Unseal(cfg, id, false, nil, WithRemap(RemapOff)); err != nil {
		t.Fatalf("Unseal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstClaude, foreignRel)); err != nil {
		t.Errorf("RemapOff should restore verbatim under %s: %v", foreignRel, err)
	}
}

// TestUnseal_InteractiveResolverInjected verifies the RemapInteractive seam:
// the injected resolver's edited target is honored and persisted as a
// device-local override.
func TestUnseal_InteractiveResolverInjected(t *testing.T) {
	sealDir, id, _ := sealForeignProject(t)

	prev := DefaultRemapResolver
	defer func() { DefaultRemapResolver = prev }()
	DefaultRemapResolver = func(plans []RemapPlan) ([]RemapPlan, error) {
		for i := range plans {
			plans[i].DstKey = "-Users-custom-proj"
			plans[i].Accepted = true
		}
		return plans, nil
	}

	dstClaude := filepath.Join(t.TempDir(), ".claude")
	cfg := config.DefaultConfig(dstClaude, sealDir)

	if _, err := Unseal(cfg, id, false, nil, WithRemap(RemapInteractive)); err != nil {
		t.Fatalf("Unseal: %v", err)
	}
	editedPath := filepath.Join(dstClaude, "projects", "-Users-custom-proj", "a.jsonl")
	if _, err := os.Stat(editedPath); err != nil {
		t.Errorf("resolver-edited target %s not restored: %v", editedPath, err)
	}
	if got := LoadOverrides(sealDir)["-home-daniel-core-enclaude"]; got != "-Users-custom-proj" {
		t.Errorf("override not persisted: got %q", got)
	}
}

// TestUnsealStatus_RemapHonest verifies the dry-run preview reports the local
// (remapped) key, not the foreign one, so `unseal --dry-run` matches reality.
func TestUnsealStatus_RemapHonest(t *testing.T) {
	sealDir, _, foreignRel := sealForeignProject(t)

	dstHome := t.TempDir()
	dstClaude := filepath.Join(dstHome, ".claude")
	cfg := config.DefaultConfig(dstClaude, sealDir)

	diff, err := UnsealStatus(cfg)
	if err != nil {
		t.Fatalf("UnsealStatus: %v", err)
	}
	localKey := "projects/" + encodePath(dstHome) + "-core-enclaude/a.jsonl"
	var sawLocal, sawForeign bool
	for _, p := range diff.Added {
		if p == localKey {
			sawLocal = true
		}
		if p == foreignRel {
			sawForeign = true
		}
	}
	if !sawLocal {
		t.Errorf("Added should list the local key %q; got %v", localKey, diff.Added)
	}
	if sawForeign {
		t.Errorf("Added should not list the foreign key %q", foreignRel)
	}
}
