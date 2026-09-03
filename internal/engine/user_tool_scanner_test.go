package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDistillUserContentXML(t *testing.T) {
	e := NewEngine(DefaultMemoCapacity)

	var noise strings.Builder
	noise.WriteString("============================= test session starts ==============================\n")
	noise.WriteString("platform linux -- Python 3.11.2, pytest-7.4.0\n")
	for i := 0; i < 200; i++ {
		noise.WriteString("tests/test_module_")
		noise.WriteString(string(rune('a' + (i % 26))))
		noise.WriteString(".py .                                                    [ 50%]\n")
	}
	noise.WriteString("FAILED tests/test_critical.py::test_crash - AssertionError: expected 200 got 500\n")
	noise.WriteString("=========================== 1 failed, 200 passed ===========================\n")

	prompt := "Please inspect this test suite run:\n<tool_output name=\"pytest\">\n" +
		noise.String() +
		"</tool_output>\nCan you fix the failing test?"

	out, ok := e.distillUserContent(0, prompt)
	if !ok {
		t.Fatal("expected distillUserContent to return true for user tool output")
	}

	if !strings.HasPrefix(out, "Please inspect this test suite run:\n<tool_output name=\"pytest\">\n") {
		t.Fatalf("human prefix was not preserved: %s", out)
	}

	if !strings.HasSuffix(out, "</tool_output>\nCan you fix the failing test?") {
		t.Fatalf("human suffix was not preserved: %s", out)
	}

	if !strings.Contains(out, "test_critical.py") {
		t.Fatal("essential failure line was lost")
	}

	if len(out) >= len(prompt) {
		t.Fatalf("expected distillation to reduce length: %d -> %d", len(prompt), len(out))
	}
}

func TestDistillUserContentPureHumanPrompt(t *testing.T) {
	e := NewEngine(DefaultMemoCapacity)

	prompt := "How do I implement an LRU cache in Go with O(1) lookups and evictions?"
	out, ok := e.distillUserContent(0, prompt)
	if ok {
		t.Fatal("expected distillUserContent to return false for pure human prompt")
	}
	if out != prompt {
		t.Fatalf("pure human prompt was modified: %q != %q", out, prompt)
	}
}

func TestApplyOpenAIChatUserToolOutput(t *testing.T) {
	e := NewEngine(DefaultMemoCapacity)

	var lockLines strings.Builder
	lockLines.WriteString("diff --git a/pnpm-lock.yaml b/pnpm-lock.yaml\nindex 123456..789abc 100644\n--- a/pnpm-lock.yaml\n+++ b/pnpm-lock.yaml\n")
	for i := 0; i < 150; i++ {
		lockLines.WriteString("+  integrity: sha512-abcdef1234567890==\n")
	}

	content := "Why did dependencies change?\n```diff\n" + lockLines.String() + "```\nCan we revert this?"

	reqObj := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": content,
			},
		},
	}
	req, err := json.Marshal(reqObj)
	if err != nil {
		t.Fatal(err)
	}

	out, res := e.Apply(req)
	if res.BlocksSqueezed == 0 {
		t.Fatal("expected BlocksSqueezed > 0 for user message with lockfile diff")
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Why did dependencies change?") {
		t.Fatal("human question prefix was lost")
	}
	if !strings.Contains(outStr, "Can we revert this?") {
		t.Fatal("human question suffix was lost")
	}
	if !strings.Contains(outStr, "pnpm-lock.yaml diff elided") {
		t.Fatalf("lockfile diff was not squozed: %s", outStr)
	}
}
