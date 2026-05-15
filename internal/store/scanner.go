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
	// ModTime as Unix timestamp (milliseconds).
	ModTimeMs int64
}

// ScanFiles walks the claude directory and returns files matching
// include patterns but not matching exclude patterns.
func ScanFiles(claudeDir string, includes, excludes []string) ([]ScanResult, error) {
	var results []ScanResult
	var walkErrors int

	compiledIncludes := compilePatterns(includes)
	compiledExcludes := compilePatterns(excludes)

	err := filepath.Walk(claudeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Count errors for paths that could contain managed files.
			// For directories: any non-excluded dir could have included
			// descendants. For files: check include/exclude directly.
			if rel, relErr := filepath.Rel(claudeDir, path); relErr == nil && rel != "." {
				excluded := matchesAnyCompiled(rel, compiledExcludes) || matchesAnyCompiled(rel+"/", compiledExcludes)
				if !excluded {
					walkErrors++
				}
			}
			return nil // continue scanning other files
		}
		if info.IsDir() {
			rel, _ := filepath.Rel(claudeDir, path)
			if rel == "." {
				return nil
			}
			// Skip entire excluded directories for performance
			if matchesAnyCompiled(rel+"/", compiledExcludes) {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(claudeDir, path)
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

		results = append(results, ScanResult{
			RelPath:   rel,
			AbsPath:   path,
			Size:      info.Size(),
			ModTimeMs: info.ModTime().UnixMilli(),
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
	var pathSegs []string
	var segsComputed bool

	for _, p := range patterns {
		if !p.hasDoubleStar {
			matched, _ := filepath.Match(p.raw, relPath)
			if matched {
				return true
			}
		} else {
			if !segsComputed {
				pathSegs = strings.Split(relPath, "/")
				segsComputed = true
			}
			if matchSegments(pathSegs, p.segs) {
				return true
			}
		}
	}
	return false
}

// MatchGlob matches a path against a glob pattern with ** support.
// It splits both path and pattern into segments and matches segment-by-segment.
func MatchGlob(path, pattern string) bool {
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	pathSegs := strings.Split(path, "/")
	patSegs := strings.Split(pattern, "/")
	return matchSegments(pathSegs, patSegs)
}

// matchSegments recursively matches path segments against pattern segments.
// Handles ** as "zero or more directory levels".
func matchSegments(pathSegs, patSegs []string) bool {
	for len(patSegs) > 0 {
		pat := patSegs[0]

		if pat == "**" {
			// ** matches zero or more path segments
			patSegs = patSegs[1:]

			// If ** is the last pattern segment, match everything remaining
			if len(patSegs) == 0 {
				return true
			}

			// Try matching the rest of the pattern at every position
			for i := 0; i <= len(pathSegs); i++ {
				if matchSegments(pathSegs[i:], patSegs) {
					return true
				}
			}
			return false
		}

		// No more path segments but pattern still has non-** segments
		if len(pathSegs) == 0 {
			return false
		}

		// Match current segment with filepath.Match (handles * and ? within a segment)
		matched, _ := filepath.Match(pat, pathSegs[0])
		if !matched {
			return false
		}

		pathSegs = pathSegs[1:]
		patSegs = patSegs[1:]
	}

	// Pattern exhausted — path must also be exhausted
	return len(pathSegs) == 0
}
