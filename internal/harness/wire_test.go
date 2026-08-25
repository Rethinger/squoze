package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWireOpenCodeCreatesAndMerges(t *testing.T) {
	home := t.TempDir()
	path, changed, err := WireOpenCode(home, "anthropic", "localhost:8787")
	if err != nil || !changed {
		t.Fatalf("wire fresh: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("invalid json: %v", string(raw))
	}
	prov := root["provider"].(map[string]any)["anthropic"].(map[string]any)
	if prov["options"].(map[string]any)["baseURL"] != "http://localhost:8787" {
		t.Fatalf("baseURL not set: %v", prov)
	}

	// Existing config with other keys must survive the merge.
	write(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		`{"model":"x/y","provider":{"openai":{"options":{"apiKey":"k"}}}}`)
	_, changed, err = WireOpenCode(home, "anthropic", "localhost:9999")
	if err != nil || !changed {
		t.Fatalf("re-wire: %v %v", changed, err)
	}
	raw, _ = os.ReadFile(path)
	var root2 map[string]any
	_ = json.Unmarshal(raw, &root2)
	if root2["model"] != "x/y" {
		t.Fatal("unrelated keys lost")
	}
	oai := root2["provider"].(map[string]any)["openai"].(map[string]any)
	if oai["options"].(map[string]any)["apiKey"] != "k" {
		t.Fatal("sibling provider damaged")
	}
}

func TestWireOpenCodeJSONCGuidesManual(t *testing.T) {
	home := t.TempDir()
	jsonc := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	write(t, jsonc, "{ // my comments\n \"model\": \"a/b\"\n}")
	_, changed, err := WireOpenCode(home, "anthropic", "localhost:8787")
	if err == nil || changed {
		t.Fatalf("jsonc must guide manual edit, got changed=%v err=%v", changed, err)
	}
	if raw, _ := os.ReadFile(jsonc); !strings.Contains(string(raw), "my comments") {
		t.Fatal("jsonc was modified")
	}
}

func TestWireOpenCodeAllWiresCatalogAndConfig(t *testing.T) {
	home := t.TempDir()
	// Config has ONE custom provider with baseURL; catalog providers are absent.
	write(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		`{"model":"p/m","provider":{"p":{"options":{"baseURL":"https://real.example/api/v1"}}}}`)

	path, wired, skipped, err := WireOpenCodeAll(home, 8787)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		t.Fatal("invalid json after all-wire")
	}
	provs := root["provider"].(map[string]any)

	// Custom provider: original captured into header, port assigned (order
	// is alphabetical across config+catalog, so assert shape, not port).
	p := provs["p"].(map[string]any)["options"].(map[string]any)
	if !strings.HasPrefix(p["baseURL"].(string), "http://localhost:") || !strings.HasSuffix(p["baseURL"].(string), "/api/v1") {
		t.Fatalf("custom provider baseURL = %v", p["baseURL"])
	}
	if p["headers"].(map[string]any)["X-Squoze-Upstream"] != "https://real.example/api/v1" {
		t.Fatalf("custom header = %v", p["headers"])
	}

	// Catalog provider without config entry: created from the defaults table.
	an := provs["anthropic"].(map[string]any)["options"].(map[string]any)
	if an["baseURL"] != "http://localhost:8787" {
		t.Fatalf("catalog baseURL = %v", an["baseURL"])
	}
	if an["headers"].(map[string]any)["X-Squoze-Upstream"] != "https://api.anthropic.com" {
		t.Fatalf("catalog header = %v", an["headers"])
	}

	if len(wired) < 10 { // custom + full catalog table
		t.Fatalf("too few wired: %v", wired)
	}
	// Every wired provider carries its own port and original URL.
	seen := map[string]bool{}
	for _, wp := range wired {
		if wp.Original == "" || wp.Addr == "" {
			t.Fatalf("wired entry incomplete: %+v", wp)
		}
		if seen[wp.Addr] {
			t.Fatalf("duplicate addr %s", wp.Addr)
		}
		seen[wp.Addr] = true
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
}

func TestWireOpenCodeAllSkipsUnknown(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		`{"provider":{"mystery":{}}}`)
	_, wired, skipped, err := WireOpenCodeAll(home, 8787)
	if err != nil {
		t.Fatal(err)
	}
	for _, wp := range wired {
		if wp.ID == "mystery" {
			t.Fatal("mystery must be skipped, not wired")
		}
	}
	if _, ok := skipped["mystery"]; !ok {
		t.Fatalf("mystery must be reported in skipped: %v", skipped)
	}
}

func TestUnwireOpenCodeRestoresBackup(t *testing.T) {
	home := t.TempDir()
	orig := `{"model":"keep-me"}`
	write(t, filepath.Join(home, ".config", "opencode", "opencode.json"), orig)
	if _, _, err := WireOpenCode(home, "anthropic", "localhost:1"); err != nil {
		t.Fatal(err)
	}
	path, restored, err := UnwireOpenCode(home)
	if err != nil || !restored {
		t.Fatalf("unwire: %v %v", restored, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != orig {
		t.Fatalf("backup restore mismatch: %s", raw)
	}
}

func TestWireOMPMergesPreservingComments(t *testing.T) {
	home := t.TempDir()
	yml := filepath.Join(home, ".omp", "agent", "models.yml")
	write(t, yml, "# my hand-written header\nproviders:\n  mine:\n    baseUrl: http://x/v1\n    auth: none\n")
	path, changed, err := WireOMP(home, "anthropic", "localhost:8787", "ANTHROPIC_API_KEY")
	if err != nil || !changed {
		t.Fatalf("wire omp: %v %v", changed, err)
	}
	if path != yml {
		t.Fatalf("path = %s", path)
	}
	raw, _ := os.ReadFile(yml)
	s := string(raw)
	for _, want := range []string{"my hand-written header", "mine:", "http://localhost:8787/v1", "apiKey: ANTHROPIC_API_KEY", "disableStrictTools: true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("models.yml missing %q:\n%s", want, s)
		}
	}
}

func TestUnwireOMPRestores(t *testing.T) {
	home := t.TempDir()
	yml := filepath.Join(home, ".omp", "agent", "models.yml")
	orig := "providers:\n  mine:\n    auth: none\n"
	write(t, yml, orig)
	if _, _, err := WireOMP(home, "anthropic", "localhost:1", "K"); err != nil {
		t.Fatal(err)
	}
	_, restored, err := UnwireOMP(home)
	if err != nil || !restored {
		t.Fatalf("unwire: %v %v", restored, err)
	}
	raw, _ := os.ReadFile(yml)
	if strings.Contains(string(raw), "localhost:1") {
		t.Fatal("unwire left wiring behind")
	}
}
