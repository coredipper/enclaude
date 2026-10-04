package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// Manifest tracks all files in the seal store with their content hashes and metadata.
type Manifest struct {
	Version  int    `json:"version"`
	DeviceID string `json:"device_id"`
	SealedAt string `json:"sealed_at"`
	// OriginHome is the absolute home directory of the machine that sealed this
	// manifest. It is the authoritative signal for the cross-device project-key
	// remap (see remap.go): the projects/ keys encode this home, and on another
	// machine they must be rewritten to the local one. Optional — older stores
	// (and the heuristic fallback) work without it; it self-populates on the
	// next seal from an updated binary.
	OriginHome string               `json:"origin_home,omitempty"`
	Files      map[string]FileEntry `json:"files"`

	// encrypted records whether the manifest was read from an encrypted
	// file, so Seal can rewrite it when encrypt_names is switched.
	encrypted bool
}

// FileEntry describes a single file in the seal store.
type FileEntry struct {
	ContentHash   string `json:"content_hash"`
	SizePlaintext int64  `json:"size_plaintext"`
	SizeEncrypted int64  `json:"size_encrypted"`
	Mtime         string `json:"mtime"`
	// ModTimeNs is the file's mtime in nanoseconds since epoch. Stored
	// alongside Mtime so the Status/UnsealStatus fast path can compare at
	// the highest resolution the filesystem exposes — RFC3339 truncates to
	// seconds and millisecond precision still aliases sub-millisecond writes,
	// either of which would let same-window content changes slip past.
	ModTimeNs     int64  `json:"mtime_ns,omitempty"`
	MergeStrategy string `json:"merge_strategy"`
	// For JSONL files, track line count for efficient dedup merge
	JSONLLineCount int `json:"jsonl_line_count,omitempty"`
	// Whether a session file is complete (immutable)
	SessionComplete bool `json:"session_complete,omitempty"`
}

// NewManifest creates an empty manifest for the given device.
func NewManifest(deviceID string) *Manifest {
	return &Manifest{
		Version:  2,
		DeviceID: deviceID,
		SealedAt: time.Now().UTC().Format(time.RFC3339),
		Files:    make(map[string]FileEntry),
	}
}

// ageHeader starts every age file. A JSON manifest starts with "{", so the
// header alone tells an encrypted manifest from a plaintext one.
const ageHeader = "age-encryption.org/"

// manifestIdentity supplies the key for an encrypted manifest when the caller
// has none, as Seal and Status only hold the public key. Tests replace it.
var manifestIdentity = func() (age.Identity, error) {
	id, _, err := crypto.LoadKey()
	if err != nil {
		return nil, err
	}
	return id, nil
}

// LoadManifest reads a manifest from disk. An encrypted manifest is
// decrypted with ids, or with the stored key when ids is empty.
func LoadManifest(sealDir string, ids ...age.Identity) (*Manifest, error) {
	path := filepath.Join(sealDir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no manifest yet
		}
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return ParseManifest(data, ids...)
}

// ParseManifest decodes manifest bytes, plaintext or encrypted, as read from
// disk or from a git ref.
func ParseManifest(data []byte, ids ...age.Identity) (*Manifest, error) {
	encrypted := bytes.HasPrefix(data, []byte(ageHeader))
	if encrypted {
		if len(ids) == 0 {
			id, err := manifestIdentity()
			if err != nil {
				return nil, fmt.Errorf("manifest is encrypted: %w", err)
			}
			ids = []age.Identity{id}
		}
		var err error
		if data, err = crypto.Decrypt(data, ids...); err != nil {
			return nil, fmt.Errorf("decrypting manifest: %w", err)
		}
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}
	m.encrypted = encrypted
	return &m, nil
}

// NameRecipients returns who manifest.json is encrypted to: r when the
// config hides folder and file names, otherwise nobody, so it stays
// plaintext.
func NameRecipients(cfg *config.Config, r age.Recipient) []age.Recipient {
	if !cfg.Seal.EncryptNames {
		return nil
	}
	return []age.Recipient{r}
}

// Marshal encodes the manifest, encrypted to recipients when there are any.
func (m *Manifest) Marshal(recipients ...age.Recipient) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest: %w", err)
	}
	if len(recipients) == 0 {
		return data, nil
	}
	return crypto.Encrypt(data, recipients...)
}

// Save writes the manifest to disk, encrypted to recipients when there are
// any (see NameRecipients).
func (m *Manifest) Save(sealDir string, recipients ...age.Recipient) error {
	m.SealedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := m.Marshal(recipients...)
	if err != nil {
		return err
	}
	if err := writeManifest(sealDir, data); err != nil {
		return err
	}
	m.encrypted = len(recipients) > 0
	return nil
}

// writeManifest replaces manifest.json with data.
func writeManifest(sealDir string, data []byte) error {
	// Write beside the manifest and rename over it, so a command reading the
	// manifest while a hook seals, or a crash mid-write, never sees half a
	// file. That would fail to parse, and an encrypted one fails to decrypt.
	f, err := os.CreateTemp(sealDir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }() // no-op once renamed
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := os.Rename(f.Name(), filepath.Join(sealDir, "manifest.json")); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// DiffResult describes the differences between two manifests.
type DiffResult struct {
	Added    []string // files in new but not old
	Modified []string // files in both but with different hashes
	Deleted  []string // files in old but not new
}

// Diff compares this manifest against another and returns the differences.
func (m *Manifest) Diff(other *Manifest) DiffResult {
	var result DiffResult

	if other == nil {
		for path := range m.Files {
			result.Added = append(result.Added, path)
		}
		return result
	}

	// Find added and modified
	for path, entry := range m.Files {
		oldEntry, exists := other.Files[path]
		if !exists {
			result.Added = append(result.Added, path)
		} else if entry.ContentHash != oldEntry.ContentHash {
			result.Modified = append(result.Modified, path)
		}
	}

	// Find deleted
	for path := range other.Files {
		if _, exists := m.Files[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}

	return result
}
