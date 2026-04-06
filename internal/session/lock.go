package session

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// VaultLock provides file-based locking for vault operations.
type VaultLock struct {
	flock *flock.Flock
}

// NewVaultLock creates a lock for the given vault directory.
func NewVaultLock(vaultDir string) *VaultLock {
	lockPath := filepath.Join(vaultDir, ".vault.lock")
	return &VaultLock{
		flock: flock.New(lockPath),
	}
}

// Acquire tries to acquire the lock with a timeout.
// Returns true if acquired, false if timed out.
func (l *VaultLock) Acquire(timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	locked, err := l.flock.TryLockContext(ctx, 200*time.Millisecond)
	if err != nil {
		return false, fmt.Errorf("acquiring vault lock: %w", err)
	}
	return locked, nil
}

// Release releases the lock.
func (l *VaultLock) Release() error {
	return l.flock.Unlock()
}
