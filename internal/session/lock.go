package session

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// SealLock provides file-based locking for seal operations.
type SealLock struct {
	flock *flock.Flock
}

// NewSealLock creates a lock for the given seal directory.
func NewSealLock(sealDir string) *SealLock {
	lockPath := filepath.Join(sealDir, ".seal.lock")
	return &SealLock{
		flock: flock.New(lockPath),
	}
}

// Acquire tries to acquire the lock with a timeout.
// Returns true if acquired, false if timed out.
func (l *SealLock) Acquire(timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	locked, err := l.flock.TryLockContext(ctx, 200*time.Millisecond)
	if err != nil {
		return false, fmt.Errorf("acquiring seal lock: %w", err)
	}
	return locked, nil
}

// Release releases the lock.
func (l *SealLock) Release() error {
	return l.flock.Unlock()
}
