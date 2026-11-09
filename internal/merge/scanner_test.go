package merge

import (
	"encoding/json"
	"testing"
)

func TestExtractSessionId(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"basic", `{"sessionId":"123"}`, "123"},
		{"second key", `{"other":"abc", "sessionId":"456"}`, "456"},
		{"nested ignored", `{"nested":{"sessionId":"789"}}`, ""},
		{"dashes", `{"sessionId":"abc-def-ghi"}`, "abc-def-ghi"},
		{"spaces around value", `{"sessionId": "with spaces" }`, "with spaces"},
		{"null value", `{"sessionId":null}`, ""},
		{"escaped quote in value", `{"sessionId": "escaped\"val" }`, `escaped"val`},
		{"spaces around colon", `{"sessionId":  "123"  }`, "123"},
		{"complex realistic", `{"version": 1, "metadata": {"sessionId": "not-this-one", "some": "data"}, "something": "sessionId", "sessionId" :  "sess-12345678-abcd-efgh-ijkl-mnopqrstuvwx", "other": [1,2,3]}`, "sess-12345678-abcd-efgh-ijkl-mnopqrstuvwx"},
        {"string value containing sessionid key", `{"key":"\"sessionId\": \"wrong\"","sessionId":"right"}`, "right"},
        {"string value containing sessionid key (raw bytes)", `{"key": "sessionId : null", "sessionId": "real-id"}`, "real-id"},
        {"string value mimicking key exactly", `{"message": "\"sessionId\" :", "sessionId": "real-id"}`, "real-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := extractSessionId(json.RawMessage(tt.input))
			if actual != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}
