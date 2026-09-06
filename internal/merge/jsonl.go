package merge

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// MergeJSONL merges two JSONL byte slices by deduplicating lines.
// Lines are deduplicated by SHA-256 of their normalized JSON content.
// The result is sorted by the "timestamp" field if present.
func MergeJSONL(ours, theirs []byte) ([]byte, error) {
	seen := make(map[[32]byte]struct{}) // hash -> seen
	var entries []jsonlEntry

	// Cache to avoid unmarshaling and hashing the exact same string repeatedly.
	// history.jsonl is append-only, so the majority of lines from `theirs`
	// will exactly match the raw bytes of earlier lines from `ours` or themselves.
	// We use the already computed hash as the key to bound memory usage and avoid
	// unbounded string map allocations. We just map the raw line's hash to the parsed timestamp.
	type parsedLine struct {
		hash [32]byte
		ts   float64
	}
	rawCache := make(map[[32]byte]parsedLine)

	// Create a single map and reuse it across all lines to eliminate
	// map allocation overhead in parseJSONLineBytes. We allocate it once here.
	objMap := make(map[string]interface{})

	for _, data := range [][]byte{ours, theirs} {
		// Optimization: avoid strings.Split to eliminate slice allocations.
		// Instead walk the byte slice by checking for \n manually.
		for len(data) > 0 {
			var lineBytes []byte
			idx := bytes.IndexByte(data, '\n')

			if idx == -1 {
				lineBytes = data
				data = nil
			} else {
				lineBytes = data[:idx]
				data = data[idx+1:]
			}

			lineBytes = bytes.TrimSpace(lineBytes)
			if len(lineBytes) == 0 {
				continue
			}

			// Calculate the cheap raw hash first
			rawHash := sha256.Sum256(lineBytes)

			var hash [32]byte
			var timestamp float64

			if cached, ok := rawCache[rawHash]; ok {
				hash = cached.hash
				timestamp = cached.ts
			} else {
				hash, timestamp = parseJSONLineBytes(lineBytes, objMap)
				rawCache[rawHash] = parsedLine{hash, timestamp}
			}

			if _, exists := seen[hash]; exists {
				continue
			}
			seen[hash] = struct{}{}

			entries = append(entries, jsonlEntry{
				line:      lineBytes,
				timestamp: timestamp,
			})
		}
	}

	// Sort by timestamp
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].timestamp < entries[j].timestamp
	})

	// Optimization: preallocate capacity based on exact size
	totalSize := 0
	for _, e := range entries {
		totalSize += len(e.line) + 1
	}

	result := make([]byte, 0, totalSize)
	for _, e := range entries {
		result = append(result, e.line...)
		result = append(result, '\n')
	}

	return result, nil
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
	line      []byte
	timestamp float64
}

// parseJSONLineBytes parses a JSON line once to extract both a normalized hash
// and a timestamp, avoiding duplicate unmarshaling overhead.
func parseJSONLineBytes(line []byte, obj map[string]interface{}) ([32]byte, float64) {
	// Clear the pre-allocated map for reuse
	for k := range obj {
		delete(obj, k)
	}

	if err := json.Unmarshal(line, &obj); err != nil {
		// Not valid JSON — hash the raw line
		return sha256.Sum256(line), 0
	}

	// Calculate timestamp
	var ts float64
	if tsFloat, ok := obj["timestamp"].(float64); ok {
		ts = tsFloat
	} else if tsStr, ok := obj["timestamp"].(string); ok {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, tsStr); err == nil {
				ts = float64(t.UnixNano())
				break
			}
		}
	}

	normalized, err := json.Marshal(obj)
	if err != nil {
		return sha256.Sum256(line), ts
	}

	// Optimize hash creation by returning byte array instead of hex string
	return sha256.Sum256(normalized), ts
}

func extractSessionId(raw json.RawMessage) string {
	// Fast-path to avoid json.Unmarshal reflection and map allocations overhead.
	// Sessions index files contain flat JSON objects.
	idx := bytes.Index(raw, []byte(`"sessionId":"`))
	if idx == -1 {
		idx = bytes.Index(raw, []byte(`"sessionId": "`))
		if idx == -1 {
			return ""
		}
		idx += 14 // len(`"sessionId": "`)
	} else {
		idx += 13 // len(`"sessionId":"`)
	}

	end := bytes.IndexByte(raw[idx:], '"')
	if end == -1 {
		return ""
	}
	return string(raw[idx : idx+end])
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
