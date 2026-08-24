package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func fakeToolOutput(lines int) string {
	var b strings.Builder
	b.WriteString("$ go test ./...\n")
	for i := 0; i < lines; i++ {
		if i%50 == 0 {
			b.WriteString("--- FAIL: TestPayment (0.01s)\n    payment_test.go:44: balance mismatch\n")
		}
		b.WriteString("ok  verbose test output line with plenty of padding padding padding\n")
	}
	return b.String()
}

func TestProcessSqueezesOpenAIToolOutput(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[` +
		`{"role":"user","content":"run tests"},` +
		`{"role":"assistant","tool_calls":[{"id":"t1","function":{"name":"bash","arguments":"{\"cmd\":\"go test\"}"}}]},` +
		`{"role":"tool","tool_call_id":"t1","content":` + mustJSON(fakeToolOutput(400)) + `}]}`)

	body, res := Process(body)

	if res.SavedBytes <= 0 || res.BlocksSqueezed != 1 {
		t.Fatalf("no squeeze: saved=%d blocks=%d", res.SavedBytes, res.BlocksSqueezed)
	}
	if res.SentBytes != len(body)-res.SavedBytes && res.SentBytes != len(body) {
		t.Fatalf("accounting mismatch: orig=%d sent=%d saved=%d", res.OriginalBytes, res.SentBytes, res.SavedBytes)
	}
	if !json.Valid(body) {
		t.Fatal("output is not valid JSON")
	}

	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Model != "gpt-5" {
		t.Fatalf("model field damaged: %q err=%v", probe.Model, err)
	}
	content := gjson.GetBytes(body, "messages.2.content").String()
	if !strings.Contains(content, "[... squoze:") {
		t.Fatal("marker missing in squeezed content")
	}
	if !strings.Contains(content, "--- FAIL") {
		t.Fatal("error line lost")
	}
	// untouched user message stays byte-identical
	if got := gjson.GetBytes(body, "messages.0.content").String(); got != "run tests" {
		t.Fatalf("user message mutated: %q", got)
	}
}

func TestProcessSqueezesAnthropicToolResultString(t *testing.T) {
	body := []byte(`{"model":"claude","system":"be nice","max_tokens":10,"messages":[` +
		`{"role":"user","content":"run tests"},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":` + mustJSON(fakeToolOutput(400)) + `}]}]}`)

	body, res := Process(body)
	if res.BlocksSqueezed != 1 || res.SavedBytes <= 0 {
		t.Fatalf("anthropic string form not squeezed: %+v", res)
	}
	out := gjson.GetBytes(body, "messages.1.content.0.content").String()
	if !strings.Contains(out, markerPrefixOf()) {
		t.Fatal("marker missing")
	}
}

func TestProcessSqueezesAnthropicTextBlocks(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":` + mustJSON(fakeToolOutput(400)) + `}]}]}]}`)

	body, res := Process(body)
	if res.BlocksSqueezed != 1 || res.SavedBytes <= 0 {
		t.Fatalf("anthropic block form not squeezed: %+v", res)
	}
	out := gjson.GetBytes(body, "messages.0.content.0.content.0.text").String()
	if !strings.Contains(out, markerPrefixOf()) {
		t.Fatal("marker missing")
	}
}

func TestProcessNeverTouchesProse(t *testing.T) {
	longProse := strings.Repeat("Please carefully review the deployment plan and consider rollback strategy. ", 60) // ~5KB prose
	body := []byte(`{"model":"m","messages":[{"role":"user","content":` + mustJSON(longProse) + `},{"role":"assistant","content":"ok"},{"role":"tool","content":` + mustJSON(longProse) + `}]}`)
	before := len(body)
	body, res := Process(body)
	if res.BlocksSqueezed != 0 || len(body) != before {
		t.Fatalf("prose must pass through: %+v", res)
	}
}

func TestProcessFailOpenOnInvalidJSON(t *testing.T) {
	broken := []byte(`{"model": broken`)
	out, res := Process(broken)
	if res.SentBytes != len(broken) || res.Format.String() != "unknown" || string(out) != string(broken) {
		t.Fatalf("fail-open violated: %+v out=%q", res, out)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func markerPrefixOf() string { return "[... squoze:" }
