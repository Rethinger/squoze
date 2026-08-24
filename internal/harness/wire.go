// Automatic config wiring for config-file agents. Every write is preceded
// by a one-time backup (<file>.squoze-bak) and can be reverted with
// Unwire*, which restores that backup.
//
// Honest limitation: opencode.jsonc files containing comments are NOT
// auto-edited (strict JSON only) — the caller falls back to printing the
// snippet for a manual paste.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const backupSuffix = ".squoze-bak"

func userHome() (string, error) { return os.UserHomeDir() }

// --- opencode -------------------------------------------------------------

// OpenCodeConfigPath returns the active global opencode config path and
// whether it already exists. Prefers .json over .jsonc (see package docs).
func OpenCodeConfigPath(home string) (string, bool) {
	dir := filepath.Join(home, ".config", "opencode")
	json := filepath.Join(dir, "opencode.json")
	if _, err := os.Stat(json); err == nil {
		return json, true
	}
	jsonc := filepath.Join(dir, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err == nil {
		return jsonc, true
	}
	return json, false
}

// WireOpenCode points providerID's built-in provider entry at addr inside
// the home directory's opencode config. Creates the file when missing.
// Returns the path and whether content changed.
func WireOpenCode(home, providerID, addr string) (string, bool, error) {
	path, existed := OpenCodeConfigPath(home)
	data := []byte("{}")
	if existed {
		raw, err := os.ReadFile(path)
		if err != nil {
			return path, false, err
		}
		if filepath.Ext(path) == ".jsonc" {
			return path, false, fmt.Errorf("%s contains comments or non-strict JSON; edit manually:\n%s", path, OpenCodeSnippet(providerID, addr))
		}
		if len(strings.TrimSpace(string(raw))) > 0 {
			data = raw
		}
	}
	if err := backupOnce(path, data, existed); err != nil {
		return path, false, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return path, false, fmt.Errorf("parse %s: %w", path, err)
	}
	prov, _ := root["provider"].(map[string]any)
	if prov == nil {
		prov = map[string]any{}
		root["provider"] = prov
	}
	entry, _ := prov[providerID].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		prov[providerID] = entry
	}
	opts, _ := entry["options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
		entry["options"] = opts
	}
	prev := opts["baseURL"]
	next := "http://" + addr
	if prev == next {
		return path, false, nil // already wired
	}
	opts["baseURL"] = next
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return path, false, err
	}
	if werr := os.WriteFile(path, out, 0o644); werr != nil {
		return path, false, werr
	}
	return path, true, nil
}

// UnwireOpenCode restores the pre-wire backup of the opencode config.
func UnwireOpenCode(home string) (string, bool, error) {
	path, _ := OpenCodeConfigPath(home)
	ok, err := restoreBackup(path)
	return path, ok, err
}

// --- omp ------------------------------------------------------------------

// OMPModelsPath is the canonical omp custom-provider file.
func OMPModelsPath(home string) string {
	return filepath.Join(home, ".omp", "agent", "models.yml")
}

// WireOMP writes an override-only entry routing providerID through the
// local proxy, merging into any existing models.yml via yaml.Node so
// comments and formatting survive. apiKeyEnv names an env var so the real
// key never lands in the file.
func WireOMP(home, providerID, addr, apiKeyEnv string) (string, bool, error) {
	path := OMPModelsPath(home)
	var doc yaml.Node
	existed := false
	if raw, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		existed = true
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			return path, false, fmt.Errorf("parse %s: %w", path, uerr)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return path, false, err
	}
	if err := backupOnce(path, docToBytes(&doc), existed); err != nil {
		return path, false, err
	}

	providersNode := ensureMappingChild(&doc, "providers")
	provNode := ensureMappingChild(providersNode, providerID)
	setScalarChild(provNode, "baseUrl", "http://"+addr+"/v1")
	setScalarChild(provNode, "apiKey", apiKeyEnv)
	setBoolChild(provNode, "authHeader", true)
	setBoolChild(provNode, "disableStrictTools", true)

	out, merr := yaml.Marshal(&doc)
	if merr != nil {
		return path, false, merr
	}
	if werr := os.MkdirAll(filepath.Dir(path), 0o755); werr != nil {
		return path, false, werr
	}
	if werr := os.WriteFile(path, out, 0o644); werr != nil {
		return path, false, werr
	}
	return path, true, nil
}

// UnwireOMP restores the pre-wire backup of models.yml.
func UnwireOMP(home string) (string, bool, error) {
	path := OMPModelsPath(home)
	ok, err := restoreBackup(path)
	return path, ok, err
}

// --- shared helpers -------------------------------------------------------

func backupOnce(path string, current []byte, existed bool) error {
	bak := path + backupSuffix
	if _, err := os.Stat(bak); err == nil {
		return nil // keep the FIRST backup; never overwrite it on re-wires
	}
	if merr := os.MkdirAll(filepath.Dir(path), 0o755); merr != nil {
		return merr
	}
	if !existed {
		return os.WriteFile(bak, []byte("# created before squoze wiring\n"), 0o644)
	}
	return os.WriteFile(bak, current, 0o644)
}

func restoreBackup(path string) (bool, error) {
	bak := path + backupSuffix
	raw, err := os.ReadFile(bak)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, raw, 0o644)
}

// ensureMappingChild returns the mapping under `key`, creating the chain
// as needed (yaml.Node preserves existing comments through edits).
// Handles a document node by descending into its single mapping child.
func ensureMappingChild(doc *yaml.Node, key string) *yaml.Node {
	if doc.Kind == 0 || (doc.Kind == yaml.DocumentNode && len(doc.Content) == 0) {
		doc.Kind = yaml.DocumentNode
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{m}
	}
	if doc.Kind == yaml.DocumentNode {
		return ensureMappingChild(doc.Content[0], key)
	}
	if doc.Kind != yaml.MappingNode {
		panic("wire: unexpected YAML root kind")
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == key {
			c := doc.Content[i+1]
			if c.Kind != yaml.MappingNode {
				c.Kind = yaml.MappingNode
				c.Value = ""
				c.Tag = "!!map"
				c.Content = nil
			}
			return c
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	doc.Content = append(doc.Content, k, v)
	return v
}

func setScalarChild(mapping *yaml.Node, key, value string) {
	setTypedScalar(mapping, key, value, "!!str")
}

func setBoolChild(mapping *yaml.Node, key string, value bool) {
	setTypedScalar(mapping, key, strconv.FormatBool(value), "!!bool")
}

func setTypedScalar(mapping *yaml.Node, key, value, tag string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Tag = tag
			return
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	mapping.Content = append(mapping.Content, k, v)
}

func docToBytes(n *yaml.Node) []byte {
	if n.Kind == 0 {
		return nil
	}
	b, _ := yaml.Marshal(n)
	return b
}
