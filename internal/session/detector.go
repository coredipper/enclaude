package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

		// Optimization: Try to extract PID from filename first (e.g., "5354.json" -> 5354).
		// If the filename contains a valid PID and the process is dead, we skip
		// the expensive file read and JSON parsing entirely. The basename must
		// match "<pid>.json" exactly; loose matching (e.g. via fmt.Sscanf) would
		// treat a filename like "2026-session.json" as PID 2026 and could skip a
		// file whose actual sess.PID inside the JSON is still alive.
		var filenamePID int
		if pid, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json")); err == nil && pid > 0 {
			filenamePID = pid
		}

		if filenamePID > 0 && !isProcessAlive(filenamePID) {
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

		if sess.PID == 0 {
			sess.PID = filenamePID
		}

		if sess.PID > 0 {
			// If the parsed PID matches the filename PID, we already know it is alive
			// because we didn't skip it above. Otherwise, we check liveness.
			if sess.PID == filenamePID || isProcessAlive(sess.PID) {
				active = append(active, sess)
			}
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
