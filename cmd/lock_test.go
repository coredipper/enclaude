package cmd

import (
	"testing"
	"time"

	"github.com/coredipper/enclaude/internal/session"
)

// TestAcquireSealLockExcludesConcurrent guards that the CLI lock is mutually
// exclusive and that release is idempotent and re-acquirable.
func TestAcquireSealLockExcludesConcurrent(t *testing.T) {
	sealDir := t.TempDir()

	release, err := acquireSealLock(sealDir)
	if err != nil {
		t.Fatalf("acquireSealLock() error: %v", err)
	}

	// A second acquisition must not succeed while the first is held. Acquire
	// reports a timeout as an error, so either outcome (error or false) is fine.
	held := session.NewSealLock(sealDir)
	acquired, err := held.Acquire(150 * time.Millisecond)
	if err == nil && acquired {
		_ = held.Release()
		t.Error("seal lock was acquired twice concurrently")
	}

	release()
	release() // release must be idempotent (callers release early before push)

	again, err := acquireSealLock(sealDir)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	again()
}
