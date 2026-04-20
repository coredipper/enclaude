package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectActiveMissingSessionsDir(t *testing.T) {
	claudeDir := t.TempDir() // no sessions/ subdir
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() should not error on missing dir, got: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestDetectActiveEmptySessionsDir(t *testing.T) {
	claudeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(claudeDir, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions in empty dir, got %d", len(sessions))
	}
}

func TestDetectActiveCorruptJSONSkipped(t *testing.T) {
	claudeDir := t.TempDir()
	sessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "bad.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() should not error on corrupt JSON, got: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after skipping corrupt JSON, got %d", len(sessions))
	}
}

func TestDetectActiveDeadProcessNotReturned(t *testing.T) {
	claudeDir := t.TempDir()
	sessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		t.Fatal(err)
	}
	// PID 999999999 is guaranteed not to be a live process
	sess := ActiveSession{PID: 999999999, SessionID: "test-session"}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "999999999.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for dead PID, got %d", len(sessions))
	}
}

func TestDetectActivePIDFromFilenameDeadProcess(t *testing.T) {
	// Session JSON has PID=0; PID should be extracted from filename
	claudeDir := t.TempDir()
	sessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		t.Fatal(err)
	}
	sess := ActiveSession{PID: 0, SessionID: "pid-from-filename"}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	// 999999998 is a dead PID; extracted from filename
	if err := os.WriteFile(filepath.Join(sessDir, "999999998.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for dead PID from filename, got %d", len(sessions))
	}
}

func TestDetectActiveIgnoresDirectoriesAndNonJSON(t *testing.T) {
	claudeDir := t.TempDir()
	sessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sessDir, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "notjson.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := DetectActive(claudeDir)
	if err != nil {
		t.Fatalf("DetectActive() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions when only non-.json files present, got %d", len(sessions))
	}
}

func TestHasActiveSessionsEmptyDir(t *testing.T) {
	if HasActiveSessions(t.TempDir()) {
		t.Error("expected HasActiveSessions to be false for empty claude dir")
	}
}
