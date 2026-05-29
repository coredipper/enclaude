package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanResult represents a file found during scanning.
type ScanResult struct {
	// RelPath is the path relative to the claude directory.
	RelPath string
	// AbsPath is the absolute filesystem path.
	AbsPath string
	// Size in bytes.
	Size int64
	// ModTime as Unix timestamp (nanoseconds).
	ModTimeNs int64
}

// ScanFiles walks the claude directory and returns files matching
// include patterns but not matching exclude patterns.
func ScanFiles(claudeDir string, includes, excludes []string) ([]ScanResult, error) {
	var results []ScanResult
	var walkErrors int

	compiledIncludes := compilePatterns(includes)
	compiledExcludes := compilePatterns(excludes)

	// Optimization: Use WalkDir instead of Walk to avoid unnecessary Lstat calls for every file.
	err := filepath.WalkDir(claudeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Count errors for paths that could contain managed files.
			// For directories: any non-excluded dir could have included
			// descendants. For files: check include/exclude directly.
			if rel, relErr := fastRel(claudeDir, path); relErr == nil && rel != "." {
				excluded := matchesAnyCompiled(rel, compiledExcludes) || matchesAnyCompiled(rel+"/", compiledExcludes)
				if !excluded {
					walkErrors++
				}
			}
			return nil // continue scanning other files
		}
		if d.IsDir() {
			rel, _ := fastRel(claudeDir, path)
			if rel == "." {
				return nil
			}
			// Skip entire excluded directories for performance
			if matchesAnyCompiled(rel+"/", compiledExcludes) {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := fastRel(claudeDir, path)
		if err != nil {
			return nil
		}

		// Check exclude first (takes priority)
		if matchesAnyCompiled(rel, compiledExcludes) {
			return nil
		}

		// Check include
		if !matchesAnyCompiled(rel, compiledIncludes) {
			return nil
		}

		// Optimization: Only fetch FileInfo (which does an Lstat) after we know we want to include this file.
		info, err := d.Info()
		if err != nil {
			walkErrors++
			return nil
		}

		results = append(results, ScanResult{
			RelPath:   rel,
			AbsPath:   path,
			Size:      info.Size(),
			ModTimeNs: info.ModTime().UnixNano(),
		})
		return nil
	})

	if err != nil {
		return results, err
	}
	if walkErrors > 0 {
		return results, fmt.Errorf("scan incomplete: %d inaccessible file(s)", walkErrors)
	}
	return results, nil
}

// fastRel is a highly optimized version of filepath.Rel for the common case
// where path is a simple descendant of base. WalkDir guarantees paths are
// joined cleanly, so we can often avoid filepath.Clean and allocation overhead.
func fastRel(base, path string) (string, error) {
	if base == path {
		return ".", nil
	}
	if strings.HasPrefix(path, base) {
		if len(path) > len(base) && os.IsPathSeparator(path[len(base)]) {
			return path[len(base)+1:], nil
		}
	}
	return filepath.Rel(base, path)
}

// compiledPattern holds a pre-processed glob pattern to avoid repeatedly
// checking for double-stars and splitting the pattern string.
type compiledPattern struct {
	raw           string
	hasDoubleStar bool
	segs          []string
}

func compilePatterns(patterns []string) []compiledPattern {
	res := make([]compiledPattern, len(patterns))
	for i, p := range patterns {
		res[i] = compiledPattern{
			raw:           p,
			hasDoubleStar: strings.Contains(p, "**"),
		}
		if res[i].hasDoubleStar {
			res[i].segs = strings.Split(p, "/")
		}
	}
	return res
}

// matchesAnyCompiled checks if a relative path matches any of the compiled glob patterns.
func matchesAnyCompiled(relPath string, patterns []compiledPattern) bool {
	for _, p := range patterns {
		if !p.hasDoubleStar {
			matched, _ := filepath.Match(p.raw, relPath)
			if matched {
				return true
			}
		} else {
			if matchSegments(relPath, p.segs) {
				return true
			}
		}
	}
	return false
}

// MatchGlob matches a path against a glob pattern with ** support.
// It splits the pattern into segments and matches segment-by-segment against the path string.
func MatchGlob(path, pattern string) bool {
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	patSegs := strings.Split(pattern, "/")
	return matchSegments(path, patSegs)
}

// matchSegments recursively matches a path string against pattern segments.
// Handles ** as "zero or more directory levels".
//
// It must reproduce strings.Split(path, "/") semantics exactly: every path
// has segment count strings.Count(path, "/")+1, so "" is one (empty) segment
// and a trailing slash ("a/b/") yields a trailing empty segment ["a","b",""].
// Collapsing those into "no segments" would flip matches for trailing-slash
// directory probes (the scanner passes rel+"/") and for the empty path.
//
// We track the remaining segments as (path, more): `more` is true whenever at
// least one segment is still unconsumed, so the empty-segment tail survives
// instead of being conflated with exhaustion. We avoid strings.Split on the
// path to eliminate slice allocations.
func matchSegments(path string, patSegs []string) bool {
	return matchSegmentsRem(path, true, patSegs)
}

// matchSegmentsRem matches the remaining path segments — the comma-free runs of
// path joined by '/', with `more` indicating an unconsumed segment is present —
// against patSegs. Exhaustion is `!more`, distinct from an empty current
// segment, which mirrors strings.Split's trailing-empty and empty-path cases.
func matchSegmentsRem(path string, more bool, patSegs []string) bool {
	for len(patSegs) > 0 {
		pat := patSegs[0]

		if pat == "**" {
			// ** matches zero or more path segments
			patSegs = patSegs[1:]

			// If ** is the last pattern segment, match everything remaining
			if len(patSegs) == 0 {
				return true
			}

			// Try matching the rest of the pattern at every segment position,
			// including the position past the final segment (where !more).
			remPath, remMore := path, more
			for {
				if matchSegmentsRem(remPath, remMore, patSegs) {
					return true
				}
				if !remMore {
					break
				}
				idx := strings.IndexByte(remPath, '/')
				if idx == -1 {
					remPath, remMore = "", false
				} else {
					remPath = remPath[idx+1:]
				}
			}
			return false
		}

		// No more path segments but pattern still has non-** segments
		if !more {
			return false
		}

		// Get current segment
		var currentSegment string
		idx := strings.IndexByte(path, '/')
		if idx == -1 {
			currentSegment = path
			path, more = "", false // Last segment consumed
		} else {
			currentSegment = path[:idx]
			path = path[idx+1:] // Move past '/'; at least the tail remains
		}

		// Match current segment with filepath.Match (handles * and ? within a segment)
		matched, _ := filepath.Match(pat, currentSegment)
		if !matched {
			return false
		}

		patSegs = patSegs[1:]
	}

	// Pattern exhausted — path must also be exhausted
	return !more
}
