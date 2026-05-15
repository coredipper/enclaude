## $(date +%Y-%m-%d) - Hash Encoding Performance
**Learning:** `hex.EncodeToString(hash[:])` is approximately 2x faster and has fewer memory allocations compared to `fmt.Sprintf("%x", hash)`. This is a valuable micro-optimization for hot paths that involve hashing large numbers of files or lines.
**Action:** Default to `hex.EncodeToString` instead of `fmt.Sprintf` for formatting byte arrays as hexadecimal strings.
