package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSealLock_Basic(t *testing.T) {
	dir := t.TempDir()
	lock1 := NewSealLock(dir)
	lock2 := NewSealLock(dir)

	// Acquire lock1
	locked, err := lock1.Acquire(1 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error acquiring lock1: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire lock1")
	}

	// Try to acquire lock2, should fail due to timeout
	start := time.Now()
	locked2, err := lock2.Acquire(500 * time.Millisecond)
	if err == nil {
		t.Fatal("expected error when acquiring locked lock")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if locked2 {
		t.Fatal("expected not to acquire lock2")
	}
	elapsed := time.Since(start)
	if elapsed < 500*time.Millisecond {
		t.Fatalf("expected Acquire to block for at least timeout, blocked for %v", elapsed)
	}

	// Release lock1
	if err := lock1.Release(); err != nil {
		t.Fatalf("failed to release lock1: %v", err)
	}

	// Now lock2 should succeed
	locked2, err = lock2.Acquire(1 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error acquiring lock2: %v", err)
	}
	if !locked2 {
		t.Fatal("expected to acquire lock2")
	}
	lock2.Release()
}

func TestSealLock_ReleaseWithoutAcquire(t *testing.T) {
	dir := t.TempDir()
	lock := NewSealLock(dir)

	err := lock.Release()
	if err != nil {
		t.Fatalf("unexpected error releasing unacquired lock: %v", err)
	}
}

func TestSealLock_PermissionDenied(t *testing.T) {
	dir := t.TempDir()

	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to create readonly dir: %v", err)
	}

	lock := NewSealLock(readOnlyDir)

	locked, err := lock.Acquire(1 * time.Second)
	if locked {
		t.Fatal("expected not to acquire lock in readonly directory")
	}
	if err == nil {
		t.Fatal("expected an error due to permission denied")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
