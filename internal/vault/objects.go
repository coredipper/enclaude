package vault

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// ObjectStore manages content-addressed encrypted blobs.
// Files are stored as objects/<hash[0:2]>/<hash[2:]>.age
type ObjectStore struct {
	dir string // path to objects/ directory
}

// NewObjectStore creates an ObjectStore rooted at the given directory.
func NewObjectStore(vaultDir string) *ObjectStore {
	return &ObjectStore{dir: filepath.Join(vaultDir, "objects")}
}

// ContentHash computes the SHA-256 hash of plaintext content.
func ContentHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// ObjectPath returns the filesystem path for a given content hash.
func (s *ObjectStore) ObjectPath(hash string) string {
	return filepath.Join(s.dir, hash[:2], hash[2:]+".age")
}

// Exists checks if an object with the given hash exists.
func (s *ObjectStore) Exists(hash string) bool {
	_, err := os.Stat(s.ObjectPath(hash))
	return err == nil
}

// Write stores encrypted data at the content-addressed path.
func (s *ObjectStore) Write(hash string, encrypted []byte) error {
	path := s.ObjectPath(hash)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating object dir: %w", err)
	}
	return os.WriteFile(path, encrypted, 0600)
}

// Read returns the encrypted data for a given content hash.
func (s *ObjectStore) Read(hash string) ([]byte, error) {
	return os.ReadFile(s.ObjectPath(hash))
}

// Delete removes an object by its content hash.
func (s *ObjectStore) Delete(hash string) error {
	return os.Remove(s.ObjectPath(hash))
}

// Init creates the objects directory.
func (s *ObjectStore) Init() error {
	return os.MkdirAll(s.dir, 0700)
}

// ListAll returns all content hashes of objects on disk.
func (s *ObjectStore) ListAll() ([]string, error) {
	var hashes []string

	prefixDirs, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading objects dir: %w", err)
	}

	for _, prefix := range prefixDirs {
		if !prefix.IsDir() || len(prefix.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(s.dir, prefix.Name())
		entries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && len(name) > 4 && name[len(name)-4:] == ".age" {
				hash := prefix.Name() + name[:len(name)-4]
				hashes = append(hashes, hash)
			}
		}
	}

	return hashes, nil
}
