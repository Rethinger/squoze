package harness

import "testing"

func TestLookupByNamesAndAliases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"claude", "claude"},
		{"CLAUDE-CODE", "claude"},
		{"codex", "openai"},
		{"gpt", "openai"},
		{"gemini-cli", "gemini"},
		{"dsh", "deepseek"},
		{"fw", "fireworks"},
		{"openrouter", "openrouter"},
	}
	for _, tc := range cases {
		p, err := Lookup(tc.in)
		if err != nil {
			t.Errorf("Lookup(%q): %v", tc.in, err)
			continue
		}
		if p.Name != tc.want {
			t.Errorf("Lookup(%q) = %s, want %s", tc.in, p.Name, tc.want)
		}
	}
	if _, err := Lookup("nope"); err == nil {
		t.Fatal("unknown harness must error")
	}
}

func TestEnvForCoversAllBaseURLVars(t *testing.T) {
	p, _ := Lookup("deepseek")
	env := p.EnvFor("127.0.0.1:8787")
	if len(env) != 2 { // DEEPSEEK_BASE_URL + DEEPSEEK_API_BASE
		t.Fatalf("want 2 env entries, got %d: %v", len(env), env)
	}
	for _, e := range env {
		if !stringsHasSuffix(e, "=http://127.0.0.1:8787") {
			t.Fatalf("bad env %q", e)
		}
	}
}

func TestDefaultsAvoidDoubleVersionPath(t *testing.T) {
	// The livecheck lesson: upstream roots carry no /v1 suffix where the
	// client appends it; a doubled path breaks every request.
	for name, want := range map[string]string{
		"fireworks": "https://api.fireworks.ai/inference",
		"openai":    "https://api.openai.com",
	} {
		p, _ := Lookup(name)
		if p.DefaultUpstream != want {
			t.Errorf("%s default = %s, want %s", name, p.DefaultUpstream, want)
		}
	}
}

func stringsHasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
