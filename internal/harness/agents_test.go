package harness

import (
	"strings"
	"testing"
)

func TestLookupAgents(t *testing.T) {
	for _, n := range []string{"claude-code", "claude", "cc", "codex", "gemini", "dsh", "opencode", "oc", "omp"} {
		if _, err := LookupAgent(n); err != nil {
			t.Errorf("LookupAgent(%q): %v", n, err)
		}
	}
	if _, err := LookupAgent("nope"); err == nil {
		t.Fatal("unknown agent must error")
	}
}

func TestOpenCodeSnippetOverridesBuiltinProvider(t *testing.T) {
	s := OpenCodeSnippet("anthropic", "localhost:8787")
	for _, want := range []string{`"anthropic"`, `"baseURL": "http://localhost:8787"`, `"$schema"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("snippet missing %q:\n%s", want, s)
		}
	}
}

func TestOMPSnippetKeepsKeyOutOfFile(t *testing.T) {
	s := OMPSnippet("anthropic", "localhost:8787", "ANTHROPIC_API_KEY")
	for _, want := range []string{"baseUrl: http://localhost:8787/v1", "apiKey: ANTHROPIC_API_KEY", "authHeader: true", "disableStrictTools: true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("snippet missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "sk-") {
		t.Fatal("snippet must never contain a literal key")
	}
}
