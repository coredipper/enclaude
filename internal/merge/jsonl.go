package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MergeJSONL merges two JSONL byte slices by deduplicating lines.
// Lines are deduplicated by SHA-256 of their normalized JSON content.
// The result is sorted by the "timestamp" field if present.
func MergeJSONL(ours, theirs []byte) ([]byte, error) {
	seen := make(map[string]string) // hash -> original line
	var entries []jsonlEntry

	for _, data := range [][]byte{ours, theirs} {
		lines := splitLines(string(data))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			hash, timestamp := parseJSONLine(line)
			if _, exists := seen[hash]; exists {
				continue
			}
			seen[hash] = line

			entries = append(entries, jsonlEntry{
				line:      line,
				timestamp: timestamp,
			})
		}
	}

	// Sort by timestamp
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].timestamp < entries[j].timestamp
	})

	var result strings.Builder
	for _, e := range entries {
		result.WriteString(e.line)
		result.WriteByte('\n')
	}

	return []byte(result.String()), nil
}

// MergeSessionsIndex merges two sessions-index.json files.
// These are JSON objects with an "entries" array; we deduplicate by sessionId.
func MergeSessionsIndex(ours, theirs []byte) ([]byte, error) {
	type indexFile struct {
		Entries []json.RawMessage `json:"entries"`
		rest    map[string]json.RawMessage
	}

	oursObj, oursEntries, err := parseIndexFile(ours)
	if err != nil {
		return nil, fmt.Errorf("parsing ours: %w", err)
	}
	theirsObj, theirsEntries, err := parseIndexFile(theirs)
	if err != nil {
		return nil, fmt.Errorf("parsing theirs: %w", err)
	}

	// Deduplicate by sessionId
	seen := make(map[string]json.RawMessage)
	var order []string

	for _, entries := range [][]json.RawMessage{oursEntries, theirsEntries} {
		for _, entry := range entries {
			sid := extractSessionId(entry)
			if sid == "" {
				sid = string(entry) // fallback: use full content as key
			}
			if _, exists := seen[sid]; !exists {
				seen[sid] = entry
				order = append(order, sid)
			}
		}
	}

	// Rebuild entries in order
	merged := make([]json.RawMessage, 0, len(order))
	for _, sid := range order {
		merged = append(merged, seen[sid])
	}

	// Build output — merge top-level keys from both sides.
	// Start with theirs, then overlay ours so ours takes precedence
	// for shared keys. This preserves metadata from theirs that ours lacks.
	outObj := make(map[string]json.RawMessage)
	for k, v := range theirsObj {
		outObj[k] = v
	}
	for k, v := range oursObj {
		outObj[k] = v
	}

	entriesJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("marshaling merged entries: %w", err)
	}
	outObj["entries"] = entriesJSON

	return json.MarshalIndent(outObj, "", "  ")
}

type jsonlEntry struct {
	line      string
	timestamp float64
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// parseJSONLine parses a JSON line once to extract both a normalized hash
// and a timestamp, avoiding duplicate unmarshaling overhead.
func parseJSONLine(line string) (string, float64) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		// Not valid JSON — hash the raw line
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), 0
	}

	// Calculate timestamp
	var ts float64
	if tsRaw, ok := obj["timestamp"]; ok {
		var tsFloat float64
		if err := json.Unmarshal(tsRaw, &tsFloat); err == nil {
			ts = tsFloat
		} else {
			var tsStr string
			if err := json.Unmarshal(tsRaw, &tsStr); err == nil {
				for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
					if t, err := time.Parse(layout, tsStr); err == nil {
						ts = float64(t.UnixNano())
						break
					}
				}
			}
		}
	}

	normalized, err := json.Marshal(obj)
	if err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), ts
	}

	h := sha256.Sum256(normalized)
	return hex.EncodeToString(h[:]), ts
}

func extractSessionId(raw json.RawMessage) string {
	var obj struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.SessionID
	}
	return ""
}

func parseIndexFile(data []byte) (map[string]json.RawMessage, []json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, nil, err
	}

	entriesRaw, ok := obj["entries"]
	if !ok {
		return obj, nil, nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return nil, nil, err
	}
	return obj, entries, nil
}
