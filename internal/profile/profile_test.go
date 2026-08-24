package profile

import "testing"

func TestDetectFamilies(t *testing.T) {
	cases := []struct {
		model string
		want  Family
	}{
		{"claude-sonnet-4-20250514", Claude},
		{"anthropic/claude-3-opus", Claude},
		{"gpt-5-mini", GPT},
		{"o3", GPT},
		{"deepseek-chat", DeepSeek},
		{"deepseek-r1:32b", DeepSeek},
		{"llama-3.1-70b", Generic},
		{"mistral-large", Generic},
	}
	for _, tc := range cases {
		if got := Detect(tc.model); got != tc.want {
			t.Errorf("Detect(%q)=%v want %v", tc.model, got, tc.want)
		}
	}
}

func TestPresetsDiffer(t *testing.T) {
	c := ParamsFor(Claude)
	d := ParamsFor(DeepSeek)
	if c.MinBytes <= d.MinBytes {
		t.Fatalf("claude must be gentler than deepseek: %d vs %d", c.MinBytes, d.MinBytes)
	}
	if ParamsFor(Generic).MinBytes == 0 || ParamsFor(GPT).Marker == "" {
		t.Fatal("presets incomplete")
	}
}
