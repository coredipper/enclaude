package merge

import (
	"fmt"
	"time"
)

// Strategy defines how two versions of a file should be merged.
type Strategy string

const (
	Immutable    Strategy = "immutable"
	JSONLDedup   Strategy = "jsonl_dedup"
	LastWriteWins Strategy = "last_write_wins"
	TextMerge    Strategy = "text_merge"
)

// FileMeta carries metadata needed for merge decisions.
type FileMeta struct {
	Mtime time.Time
}

// Merge applies the given strategy to merge two file contents.
// ancestor is the common ancestor (may be nil for two-way merge).
// Returns the merged content.
func Merge(strategy Strategy, ancestor, ours, theirs []byte, oursMeta, theirsMeta FileMeta) ([]byte, error) {
	switch strategy {
	case Immutable:
		return mergeImmutable(ours, theirs)
	case JSONLDedup:
		return MergeJSONL(ours, theirs)
	case LastWriteWins:
		return mergeLastWriteWins(ours, theirs, oursMeta, theirsMeta)
	case TextMerge:
		return mergeText(ancestor, ours, theirs)
	default:
		return nil, fmt.Errorf("unknown merge strategy: %s", strategy)
	}
}

func mergeImmutable(ours, theirs []byte) ([]byte, error) {
	// Immutable files should never diverge. If they do, prefer ours.
	return ours, nil
}

func mergeLastWriteWins(ours, theirs []byte, oursMeta, theirsMeta FileMeta) ([]byte, error) {
	if theirsMeta.Mtime.After(oursMeta.Mtime) {
		return theirs, nil
	}
	return ours, nil
}

func mergeText(ancestor, ours, theirs []byte) ([]byte, error) {
	if ancestor == nil {
		// No common ancestor — can't do 3-way merge, prefer ours
		return ours, nil
	}
	// Simple 3-way merge: if theirs changed and ours didn't, take theirs
	oursChanged := string(ours) != string(ancestor)
	theirsChanged := string(theirs) != string(ancestor)

	if !oursChanged && theirsChanged {
		return theirs, nil
	}
	if oursChanged && !theirsChanged {
		return ours, nil
	}
	if !oursChanged && !theirsChanged {
		return ours, nil
	}

	// Both changed — concatenate with conflict markers
	result := fmt.Sprintf("<<<<<<< ours\n%s\n=======\n%s\n>>>>>>> theirs\n", string(ours), string(theirs))
	return []byte(result), nil
}
