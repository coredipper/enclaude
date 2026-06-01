package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
	"testing"
)

func parseJSONLineOriginal(line string) (string, float64) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), 0
	}

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
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), ts
	}

	h := sha256.Sum256(normalized)
	return hex.EncodeToString(h[:]), ts
}

func parseJSONLineFast(line string) (string, float64) {
    // We only need the timestamp, we can use a struct
	var obj struct {
        Timestamp json.RawMessage `json:"timestamp"`
    }
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), 0
	}

	var ts float64
	if len(obj.Timestamp) > 0 && string(obj.Timestamp) != "null" {
		var tsFloat float64
		if err := json.Unmarshal(obj.Timestamp, &tsFloat); err == nil {
			ts = tsFloat
		} else {
			var tsStr string
			if err := json.Unmarshal(obj.Timestamp, &tsStr); err == nil {
				for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
					if t, err := time.Parse(layout, tsStr); err == nil {
						ts = float64(t.UnixNano())
						break
					}
				}
			}
		}
	}

    // Now convert the entire line back to map[string]interface{} for canonical marshaling
    var canonicalObj map[string]interface{}
    if err := json.Unmarshal([]byte(line), &canonicalObj); err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), ts
    }

	normalized, err := json.Marshal(canonicalObj)
	if err != nil {
		h := sha256.Sum256([]byte(line))
		return hex.EncodeToString(h[:]), ts
	}

	h := sha256.Sum256(normalized)
	return hex.EncodeToString(h[:]), ts
}

func BenchmarkParseJSONLineOriginal(b *testing.B) {
	line := `{"timestamp": 123456.789, "a": "b", "c": {"d": "e"}, "some_large_field": "long string with lots of data", "nested": [{"x": 1}, {"x": 2}]}`
	for i := 0; i < b.N; i++ {
		parseJSONLineOriginal(line)
	}
}

func BenchmarkParseJSONLineFast(b *testing.B) {
	line := `{"timestamp": 123456.789, "a": "b", "c": {"d": "e"}, "some_large_field": "long string with lots of data", "nested": [{"x": 1}, {"x": 2}]}`
	for i := 0; i < b.N; i++ {
		parseJSONLineFast(line)
	}
}

func main() {
    res1 := testing.Benchmark(BenchmarkParseJSONLineOriginal)
    fmt.Printf("Original: %s - %d mem allocs/op\n", res1.String(), res1.AllocsPerOp())
    res2 := testing.Benchmark(BenchmarkParseJSONLineFast)
    fmt.Printf("Optimized: %s - %d mem allocs/op\n", res2.String(), res2.AllocsPerOp())
}
