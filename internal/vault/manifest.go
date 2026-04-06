package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manifest tracks all files in the vault with their content hashes and metadata.
type Manifest struct {
	Version  int                    `json:"version"`
	DeviceID string                 `json:"device_id"`
	SealedAt string                 `json:"sealed_at"`
	Files    map[string]FileEntry   `json:"files"`
}

// FileEntry describes a single file in the vault.
type FileEntry struct {
	ContentHash   string `json:"content_hash"`
	SizePlaintext int64  `json:"size_plaintext"`
	SizeEncrypted int64  `json:"size_encrypted"`
	Mtime         string `json:"mtime"`
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

// Load reads a manifest from disk.
func LoadManifest(vaultDir string) (*Manifest, error) {
	path := filepath.Join(vaultDir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no manifest yet
		}
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}
	return &m, nil
}

// Save writes the manifest to disk.
func (m *Manifest) Save(vaultDir string) error {
	m.SealedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	path := filepath.Join(vaultDir, "manifest.json")
	return os.WriteFile(path, data, 0600)
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
