## 2026-09-19 - Failed micro-optimization on sessionId extraction
**Learning:** Manual JSON byte scanning (`bytes.Index`) for a specific key (like `"sessionId"`) is brittle and unsafe unless strictly validated. It can falsely match the substring inside values of other unrelated keys in the JSON payload (e.g., `{"description": "The sessionId is bad"}`). Speed should never sacrifice correctness.
**Action:** Stick to `json.Unmarshal` or specialized robust parsing libraries unless a fully verifiable, zero-allocation custom parsing logic is completely isolated and proven safe.

## 2026-09-19 - Reused maps for json payloads
**Learning:** When generating a combined JSON output from multiple sources, instead of creating a brand new `map[string]json.RawMessage` and copying all keys from source A and source B, you can reuse the map from source A (e.g., `theirsObj`) to collect source B's keys.
**Action:** When merging, evaluate if one of the input maps can be mutated instead of allocating a fresh output map to save GC overhead.
