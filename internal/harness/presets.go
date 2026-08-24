// Package harness maps AI coding harnesses to their provider base-URL
// environment variables and default upstreams, so `squoze harness <name>`
// wires any of them through the proxy with zero guesswork.
//
// The subtle part each preset encodes: upstream defaults are chosen so that
// ReverseProxy path-joining produces correct endpoints (e.g. Fireworks wants
// /inference/v1/chat/completions — so its default carries no trailing /v1;
// clients add it).
package harness

import (
	"fmt"
	"sort"
)

// Preset describes one supported harness/provider family.
type Preset struct {
	Name            string   // canonical name used on the command line
	Aliases         []string // accepted alternative names
	BaseURLEnvs     []string // env vars the harness reads for the endpoint
	DefaultUpstream string   // provider root when --upstream is omitted
}

var registry = map[string]*Preset{
	"claude": {
		Name:            "claude",
		Aliases:         []string{"claude-code", "anthropic"},
		BaseURLEnvs:     []string{"ANTHROPIC_BASE_URL"},
		DefaultUpstream: "https://api.anthropic.com",
	},
	"openai": {
		Name:            "openai",
		Aliases:         []string{"codex", "gpt", "openai-sdk"},
		BaseURLEnvs:     []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"},
		DefaultUpstream: "https://api.openai.com",
	},
	"gemini": {
		Name:            "gemini",
		Aliases:         []string{"gemini-cli", "google"},
		BaseURLEnvs:     []string{"GOOGLE_GEMINI_BASE_URL"},
		DefaultUpstream: "https://generativelanguage.googleapis.com",
	},
	"deepseek": {
		Name:            "deepseek",
		Aliases:         []string{"dsh", "deepseek-official"},
		BaseURLEnvs:     []string{"DEEPSEEK_BASE_URL", "DEEPSEEK_API_BASE"},
		DefaultUpstream: "https://api.deepseek.com",
	},
	"openrouter": {
		Name:            "openrouter",
		Aliases:         nil,
		BaseURLEnvs:     []string{"OPENROUTER_BASE_URL"},
		DefaultUpstream: "https://openrouter.ai/api",
	},
	"fireworks": {
		Name:            "fireworks",
		Aliases:         []string{"fw"},
		BaseURLEnvs:     []string{"FIREWORKS_BASE_URL"},
		DefaultUpstream: "https://api.fireworks.ai/inference",
	},
}

// Lookup resolves a harness by canonical name or alias (case-insensitive).
func Lookup(name string) (*Preset, error) {
	lower := lower(name)
	if p, ok := registry[lower]; ok {
		return p, nil
	}
	for _, p := range registry {
		for _, a := range p.Aliases {
			if a == lower {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown harness %q (try: squoze harness list)", name)
}

// Names returns all canonical preset names sorted alphabetically.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EnvFor returns KEY=VALUE entries pointing every base-URL env of the preset
// at addr (host:port of the local squoze proxy).
func (p *Preset) EnvFor(addr string) []string {
	out := make([]string, 0, len(p.BaseURLEnvs))
	for _, k := range p.BaseURLEnvs {
		out = append(out, k+"=http://"+addr)
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
