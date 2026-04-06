package crypto

import (
	"crypto/rand"
	"fmt"
	"os"
)

// ShredFile overwrites a file with random data before deleting it.
// This provides better-than-nothing protection on HDDs.
// On SSDs with TRIM, this is less effective but still removes the plaintext file.
func ShredFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening %s for overwrite: %w", path, err)
	}

	// Overwrite with random data
	size := info.Size()
	buf := make([]byte, min(size, 64*1024))
	for written := int64(0); written < size; {
		n := min(int64(len(buf)), size-written)
		rand.Read(buf[:n])
		if _, err := f.Write(buf[:n]); err != nil {
			f.Close()
			return fmt.Errorf("overwriting %s: %w", path, err)
		}
		written += n
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("syncing %s: %w", path, err)
	}
	f.Close()

	return os.Remove(path)
}
