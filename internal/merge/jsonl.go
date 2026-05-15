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
	var cachedLayout string

	for _, data := range [][]byte{ours, theirs} {
		lines := splitLines(string(data))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			hash := hashNormalized(line)
			if _, exists := seen[hash]; exists {
				continue
			}
			seen[hash] = line

			entries = append(entries, jsonlEntry{
				line:      line,
				timestamp: extractTimestamp(line, &cachedLayout),
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

	oursEntries, err := parseIndexEntries(ours)
	if err != nil {
		return nil, fmt.Errorf("parsing ours: %w", err)
	}
	theirsEntries, err := parseIndexEntries(theirs)
	if err != nil {
		return nil, fmt.Errorf("parsing theirs: %w", err)
	}

	// Deduplicate by sessionId
	seen := make(map[string]json.RawMessage)
	var order []string

	for _, entries := range [][]json.RawMessage{oursEntries, theirsEntries} {
		for _, entry := range entries {
			sid := extractField(entry, "sessionId")
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
	var theirsObj, oursObj map[string]json.RawMessage
	json.Unmarshal(theirs, &theirsObj)
	json.Unmarshal(ours, &oursObj)

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

// hashNormalized computes SHA-256 of JSON with sorted keys to catch semantic duplicates.
func hashNormalized(line string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		// Not valid JSON — hash the raw line
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:])
	}

	normalized, err := json.Marshal(obj)
	if err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:])
	}

	h := sha256.Sum256(normalized)
	return hex.EncodeToString(h[:])
}

// extractTimestamp pulls the "timestamp" field from a JSON line for sorting.
func extractTimestamp(line string, cachedLayout *string) float64 {
	// Fast path: try to extract timestamp using string search to avoid full JSON parsing
	idx := strings.Index(line, `"timestamp":`)
	if idx != -1 {
		valStart := idx + 12 // len(`"timestamp":`)
		for valStart < len(line) && (line[valStart] == ' ' || line[valStart] == '\t') {
			valStart++
		}
		if valStart < len(line) && line[valStart] == '"' {
			valEnd := strings.IndexByte(line[valStart+1:], '"')
			if valEnd != -1 {
				ts := line[valStart+1 : valStart+1+valEnd]
				if cachedLayout != nil && *cachedLayout != "" {
					if t, err := time.Parse(*cachedLayout, ts); err == nil {
						return float64(t.UnixNano())
					}
				}
				for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
					if cachedLayout != nil && *cachedLayout == layout {
						continue
					}
					if t, err := time.Parse(layout, ts); err == nil {
						if cachedLayout != nil {
							*cachedLayout = layout
						}
						return float64(t.UnixNano())
					}
				}
			}
		}
	}

	// Slow path: full JSON parse
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return 0
	}

	// Try "timestamp" (history.jsonl uses Unix millis as number)
	if ts, ok := obj["timestamp"].(float64); ok {
		return ts
	}

	// Try "timestamp" as string (session JSONL uses ISO 8601 / RFC 3339 format)
	if ts, ok := obj["timestamp"].(string); ok {
		if cachedLayout != nil && *cachedLayout != "" {
			if t, err := time.Parse(*cachedLayout, ts); err == nil {
				return float64(t.UnixNano())
			}
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if cachedLayout != nil && *cachedLayout == layout {
				continue
			}
			if t, err := time.Parse(layout, ts); err == nil {
				if cachedLayout != nil {
					*cachedLayout = layout
				}
				return float64(t.UnixNano())
			}
		}
	}

	return 0
}

func extractField(raw json.RawMessage, field string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if v, ok := obj[field].(string); ok {
		return v
	}
	return ""
}

func parseIndexEntries(data []byte) ([]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}

	entriesRaw, ok := obj["entries"]
	if !ok {
		return nil, nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
