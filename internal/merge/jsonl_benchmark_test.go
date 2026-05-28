package merge

import (
	"testing"
)

func BenchmarkMergeSessionsIndex(b *testing.B) {
	ours := []byte(`{"version": 1, "entries": [{"sessionId": "1", "data": "A"}, {"sessionId": "2", "data": "B"}], "extra": "ours"}`)
	theirs := []byte(`{"version": 1, "entries": [{"sessionId": "2", "data": "C"}, {"sessionId": "3", "data": "D"}], "other": "theirs"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := MergeSessionsIndex(ours, theirs)
		if err != nil {
			b.Fatal(err)
		}
	}
}
