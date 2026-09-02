package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamScannerEdgeCases(t *testing.T) {
	e := NewEngine(DefaultMemoCapacity)

	t.Run("empty messages array", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[]}`)
		out, res := e.Apply(body)
		if !bytes.Equal(out, body) || res.SavedBytes != 0 {
			t.Fatalf("expected untouched passthrough for empty messages, got %s", string(out))
		}
		if res.DurationMS < 0 {
			t.Fatalf("invalid duration reported: %f", res.DurationMS)
		}
	})

	t.Run("tool message without content", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"tool","tool_call_id":"call_1"}]}`)
		out, res := e.Apply(body)
		if !bytes.Equal(out, body) || res.SavedBytes != 0 {
			t.Fatalf("expected untouched passthrough, got %s", string(out))
		}
	})

	t.Run("non-tool messages untouched", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"sys"},{"role":"user","content":"usr"},{"role":"assistant","content":"ast"}]}`)
		out, res := e.Apply(body)
		if !bytes.Equal(out, body) || res.SavedBytes != 0 {
			t.Fatalf("expected untouched passthrough for non-tool messages, got %s", string(out))
		}
	})

	t.Run("malformed json fail-open", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"tool","content":`)
		out, res := e.Apply(body)
		if !bytes.Equal(out, body) || res.SavedBytes != 0 {
			t.Fatalf("expected fail-open on malformed JSON")
		}
	})

	t.Run("multi-block anthropic tool results", func(t *testing.T) {
		blob1 := fakeToolOutput(300)
		blob2 := fakeToolOutput(300)
		body := []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":[` +
			`{"type":"tool_result","tool_use_id":"t1","content":` + mustJSON(blob1) + `},` +
			`{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":` + mustJSON(blob2) + `}]}` +
			`]}]}`)

		out, res := e.Apply(body)
		if res.BlocksSqueezed != 2 || res.SavedBytes <= 0 {
			t.Fatalf("expected 2 blocks squeezed, got %d, saved=%d", res.BlocksSqueezed, res.SavedBytes)
		}
		if !json.Valid(out) {
			t.Fatalf("output is not valid JSON: %s", string(out))
		}
		if !strings.Contains(string(out), "[... squoze:") {
			t.Fatalf("marker missing from squeezed output")
		}
	})
}
