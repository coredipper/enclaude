package gitops

import "testing"

// TestParsePorcelainNoOpRecognizesUpToDate confirms the `=` prefix logic
// — git emits a single `=` line per branch when nothing changed.
func TestParsePorcelainNoOpRecognizesUpToDate(t *testing.T) {
	stdout := "To git@github.com:example/repo.git\n=\trefs/heads/main:refs/heads/main\t[up to date]\nDone"
	if !parsePorcelainNoOp(stdout) {
		t.Errorf("expected NoOp=true for up-to-date porcelain output")
	}
}

// TestParsePorcelainNoOpRejectsRealPush guards against false positives
// when git advances a ref (lines start with a space or other flag, not =).
func TestParsePorcelainNoOpRejectsRealPush(t *testing.T) {
	stdout := "To git@github.com:example/repo.git\n\trefs/heads/main:refs/heads/main\tabc1234..def5678\nDone"
	if parsePorcelainNoOp(stdout) {
		t.Errorf("expected NoOp=false when a ref advanced")
	}
}

// TestParsePushTransferExtractsObjectsAndBytes covers the happy path —
// git emits a "Total N" line plus a human-readable "Writing objects ...
// SIZE UNIT" line on stderr.
func TestParsePushTransferExtractsObjectsAndBytes(t *testing.T) {
	stderr := `Enumerating objects: 12, done.
Counting objects: 100% (12/12), done.
Compressing objects: 100% (9/9), done.
Writing objects: 100% (9/9), 412.00 KiB | 8.24 MiB/s, done.
Total 9 (delta 2), reused 0 (delta 0), pack-reused 0
`
	objs, b := parsePushTransfer(stderr)
	if objs != 9 {
		t.Errorf("objects: want 9, got %d", objs)
	}
	if b != 412*1024 {
		t.Errorf("bytes: want %d, got %d", 412*1024, b)
	}
}

// TestParsePushTransferTolerantOfMissingFields keeps the parser resilient
// across git versions that omit the progress line (e.g. quiet pushes).
func TestParsePushTransferTolerantOfMissingFields(t *testing.T) {
	objs, b := parsePushTransfer("just a status line, no totals")
	if objs != 0 || b != 0 {
		t.Errorf("expected zero values for unrecognized stderr, got objs=%d bytes=%d", objs, b)
	}
}

// TestParseShortstatFilesAcceptsSingularAndPlural locks in support for
// `1 file changed` and `7 files changed`, which differ by one character.
func TestParseShortstatFilesAcceptsSingularAndPlural(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{" 1 file changed, 3 insertions(+)", 1},
		{" 7 files changed, 30 insertions(+), 12 deletions(-)", 7},
		{" 42 files changed, 1 deletion(-)", 42},
	} {
		if got := parseShortstatFiles(tc.in); got != tc.want {
			t.Errorf("parseShortstatFiles(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestParseShortstatFilesUnrecognized returns 0 rather than panicking on
// an empty diff (`--shortstat` prints nothing when there's no delta).
func TestParseShortstatFilesUnrecognized(t *testing.T) {
	if got := parseShortstatFiles(""); got != 0 {
		t.Errorf("expected 0 for empty input, got %d", got)
	}
}

// TestJoinStreamsSkipsEmptySides keeps the user-facing combined output
// free of stray blank lines when push/pull only wrote to one stream.
func TestJoinStreamsSkipsEmptySides(t *testing.T) {
	if got := joinStreams("out", ""); got != "out" {
		t.Errorf("stdout-only: got %q", got)
	}
	if got := joinStreams("", "err"); got != "err" {
		t.Errorf("stderr-only: got %q", got)
	}
	if got := joinStreams("a", "b"); got != "a\nb" {
		t.Errorf("both: got %q", got)
	}
}
