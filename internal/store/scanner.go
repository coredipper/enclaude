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
// checking for double-stars. We no longer split the pattern ahead of time
// since matchSegmentsPatRem avoids allocations by splitting on the fly.
type compiledPattern struct {
	raw           string
	hasDoubleStar bool
}

func compilePatterns(patterns []string) []compiledPattern {
	res := make([]compiledPattern, len(patterns))
	for i, p := range patterns {
		res[i] = compiledPattern{
			raw:           p,
			hasDoubleStar: strings.Contains(p, "**"),
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
			if matchSegmentsPatRem(relPath, true, p.raw, true) {
				return true
			}
		}
	}
	return false
}

// MatchGlob matches a path against a glob pattern with ** support.
// It avoids strings.Split on both path and pattern strings to eliminate slice allocations.
func MatchGlob(path, pattern string) bool {
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	return matchSegmentsPatRem(path, true, pattern, true)
}

// matchSegmentsPatRem matches the remaining path segments against the remaining
// pattern segments, handling ** as "zero or more directory levels". It walks
// both strings with strings.IndexByte instead of strings.Split to avoid slice
// allocations.
//
// morePath/morePat must reproduce strings.Split semantics exactly: Split never
// yields zero segments, so "" is one empty segment and a trailing slash
// ("a/b/") leaves a trailing empty one. Each bool means "a segment is still
// unconsumed" — true even when that segment is empty — keeping the empty tail
// distinct from exhaustion (!more). Collapsing the two would flip matches for
// the empty path and for the scanner's rel+"/" directory probes.
func matchSegmentsPatRem(path string, morePath bool, pattern string, morePat bool) bool {
	for morePat {
		var pat string
		idx := strings.IndexByte(pattern, '/')
		if idx == -1 {
			pat = pattern
			pattern, morePat = "", false
		} else {
			pat = pattern[:idx]
			pattern = pattern[idx+1:]
		}

		if pat == "**" {
			// ** matches zero or more path segments
			if !morePat {
				return true
			}

			// Try matching the rest of the pattern at every segment position
			remPath, remMore := path, morePath
			for {
				if matchSegmentsPatRem(remPath, remMore, pattern, morePat) {
					return true
				}
				if !remMore {
					break
				}
				idxPath := strings.IndexByte(remPath, '/')
				if idxPath == -1 {
					remPath, remMore = "", false
				} else {
					remPath = remPath[idxPath+1:]
				}
			}
			return false
		}

		// No more path segments but pattern still has non-** segments
		if !morePath {
			return false
		}

		// Get current segment
		var currentSegment string
		idxPath := strings.IndexByte(path, '/')
		if idxPath == -1 {
			currentSegment = path
			path, morePath = "", false // Last segment consumed
		} else {
			currentSegment = path[:idxPath]
			path = path[idxPath+1:] // Move past '/'; at least the tail remains
		}

		// Match current segment with filepath.Match (handles * and ? within a segment)
		matched, _ := filepath.Match(pat, currentSegment)
		if !matched {
			return false
		}
	}

	// Pattern exhausted — path must also be exhausted
	return !morePath
}
