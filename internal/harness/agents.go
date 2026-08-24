// Agent-level presets: the coding agents themselves (as opposed to raw
// providers). Env-driven agents reuse the provider presets via wrap;
// config-file agents (opencode, omp) get generated snippets plus a running
// proxy, because their endpoint lives in a config file, not the environment.
package harness

import "fmt"

// Agent describes one supported coding agent.
type Agent struct {
	Name    string
	Aliases []string
	// Preset is the provider preset used for env injection / defaults.
	// Empty for config-file agents that wire themselves.
	Preset string
	// Launch is the typical command that starts the agent.
	Launch string
	// Kind selects the wiring strategy: env agents get base-URL injection,
	// config agents get their config file patched automatically.
	Kind string // "env" | "opencode" | "omp"
}

var agents = map[string]*Agent{
	"claude-code": {Name: "claude-code", Aliases: []string{"claude", "cc"}, Preset: "claude", Launch: "claude", Kind: "env"},
	"codex":       {Name: "codex", Aliases: []string{"openai-codex"}, Preset: "openai", Launch: "codex", Kind: "env"},
	"gemini-cli":  {Name: "gemini-cli", Aliases: []string{"gemini"}, Preset: "gemini", Launch: "gemini", Kind: "env"},
	"dsh":         {Name: "dsh", Aliases: []string{"deepseek-harness"}, Preset: "deepseek", Launch: "dsh", Kind: "env"},
	"opencode":    {Name: "opencode", Aliases: []string{"oc"}, Launch: "opencode", Kind: "opencode"},
	"omp":         {Name: "oh-my-pi", Aliases: []string{"omp"}, Launch: "omp", Kind: "omp"},
}

// LookupAgent resolves an agent by name or alias.
func LookupAgent(name string) (*Agent, error) {
	lower := lower(name)
	if a, ok := agents[lower]; ok {
		return a, nil
	}
	for _, a := range agents {
		for _, al := range a.Aliases {
			if al == lower {
				return a, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown agent %q (try: squoze agent list)", name)
}

// AgentNames returns sorted agent names.
func AgentNames() []string {
	out := make([]string, 0, len(agents))
	for k := range agents {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// OpenCodeSnippet renders the opencode.json override that points an
// existing built-in provider through the local squoze proxy. Overriding a
// built-in provider (instead of adding a custom @ai-sdk/openai-compatible
// one) avoids the known options-drop bug (anomalyco/opencode#5674).
func OpenCodeSnippet(providerID, addr string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "%s": {
      "options": {
        "baseURL": "http://%s"
      }
    }
  }
}`, providerID, addr)
}

// OMPSnippet renders the ~/.omp/agent/models.yml override-only entry that
// reroutes an existing catalog provider through the local proxy. An entry
// without `models` patches the built-in provider in place; apiKey accepts
// an env-var NAME, so the real key never lands in the file.
// disableStrictTools matches Anthropic-compatible endpoints that reject
// strict tool-schema markers.
func OMPSnippet(providerID, addr, apiKeyEnv string) string {
	return fmt.Sprintf(`# ~/.omp/agent/models.yml — route the %s provider through local squoze
providers:
  %s:
    baseUrl: http://%s/v1
    apiKey: %s        # env var name — real key stays out of the file
    authHeader: true
    disableStrictTools: true
`, providerID, providerID, addr, apiKeyEnv)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
