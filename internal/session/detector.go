package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ActiveSession represents a running Claude Code session.
type ActiveSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Project   string `json:"project"`
}

// DetectActive reads ~/.claude/sessions/*.json and returns sessions
// whose PIDs are still alive.
func DetectActive(claudeDir string) ([]ActiveSession, error) {
	sessionsDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading sessions dir: %w", err)
	}

	var active []ActiveSession
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			continue
		}

		var sess ActiveSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		// Extract PID from filename (e.g., "5354.json" -> 5354)
		if sess.PID == 0 {
			fmt.Sscanf(entry.Name(), "%d.json", &sess.PID)
		}

		if sess.PID > 0 && isProcessAlive(sess.PID) {
			active = append(active, sess)
		}
	}

	return active, nil
}

// HasActiveSessions returns true if any Claude Code sessions are running.
func HasActiveSessions(claudeDir string) bool {
	active, err := DetectActive(claudeDir)
	if err != nil {
		return false
	}
	return len(active) > 0
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Use kill(pid, 0) to check.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
