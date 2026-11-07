package merge

import (
	"encoding/json"
	"testing"
)

func TestExtractSessionId(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"simple", `{"sessionId": "1234"}`, "1234"},
		{"with spaces", `{  "sessionId"  :   "5678" }`, "5678"},
		{"multiple keys", `{"other": "value", "sessionId": "9012"}`, "9012"},
		{"with escaped quote", `{"sessionId": "has\"quote"}`, `has"quote`},
		{"missing", `{"other": "value"}`, ""},
		{"nested match string", `{"other": "\"sessionId\": \"nope\"", "sessionId": "actual"}`, "actual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSessionId(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("extractSessionId() = %v, want %v", got, tt.want)
			}
		})
	}
}
