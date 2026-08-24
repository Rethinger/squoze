package wire

import "testing"

func TestDetectFormats(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Format
	}{
		{"openai chat", `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, FormatOpenAIChat},
		{"anthropic", `{"model":"claude","system":"be nice","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":10}`, FormatAnthropicMessages},
		{"anthropic no system, tool result", `{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"out"}]}],"max_tokens":10}`, FormatAnthropicMessages},
		{"responses instructions", `{"model":"gpt-5","instructions":"be terse","input":"hi"}`, FormatOpenAIResponses},
		{"responses input only", `{"model":"gpt-5","input":[{"role":"user"}]}`, FormatOpenAIResponses},
		{"invalid json", `{oops`, FormatUnknown},
		{"empty object", `{}`, FormatUnknown},
		{"null system is openai chat", `{"model":"m","system":null,"messages":[]}`, FormatOpenAIChat},
	}
	for _, tc := range cases {
		if got := Detect([]byte(tc.body)); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
