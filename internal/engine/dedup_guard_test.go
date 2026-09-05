package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// repeatedRead is a file the agent read twice — the shape cross-turn dedup
// exists for. Kept narrow and non-machine-output so the head/tail squeezer
// stays out of it and dedup is the only transform under test.
func repeatedRead() string {
	return strings.Repeat("func ProcessData(chunk []byte) error { return nil }\n", 60)
}

func twoReadTurns() []byte {
	blob := mustJSON(repeatedRead())
	return []byte(`{"model":"gpt-5","messages":[` +
		`{"role":"user","content":"read the file"},` +
		`{"role":"assistant","tool_calls":[{"id":"t1","function":{"name":"read","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"t1","content":` + blob + `},` +
		`{"role":"user","content":"read it again"},` +
		`{"role":"assistant","tool_calls":[{"id":"t2","function":{"name":"read","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"t2","content":` + blob + `}]}`)
}

// TestDedupCollapsesTheLaterCopy covers finding (d) and its consequence.
//
// (d) was mechanical: the scanner mutated targets[i].content in place during
// dedup, then gated the write on `out != t.content` — comparing the rewritten
// value against itself — so the replacement was silently discarded and dedup
// did nothing at all.
//
// Making it land exposed the policy bug underneath: v0.2.0 collapsed the
// EARLIEST copy, which is the one already inside the provider's cached prefix.
// The copy that may be rewritten is the newest one, never history.
func TestDedupCollapsesTheLaterCopy(t *testing.T) {
	body := twoReadTurns()
	out, _ := Process(body)

	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	first := gjson.GetBytes(out, "messages.2.content").String()
	last := gjson.GetBytes(out, "messages.5.content").String()

	if first != repeatedRead() {
		t.Errorf("the earlier copy was rewritten (%d bytes, original %d); "+
			"it sits inside the already-cached prefix",
			len(first), len(repeatedRead()))
	}
	if len(last) >= len(repeatedRead()) {
		t.Errorf("the later copy was not collapsed: %d bytes, original %d",
			len(last), len(repeatedRead()))
	}
	if !strings.Contains(last, "identical to the copy in turn") {
		t.Errorf("the later copy lost its dedup marker: %.120s", last)
	}
}

// TestDedupKeepsSentHistoryByteIdentical is the contract the 2papi harness
// pins: replay a growing conversation and every message the provider has
// already seen must come back unchanged. Process() on a single body cannot
// catch a violation here — the bug only shows across successive requests,
// which is how a real session arrives.
func TestDedupKeepsSentHistoryByteIdentical(t *testing.T) {
	turn2, _ := Process(twoReadTurns())
	turn3, _ := Process(threeReadTurns())

	for _, path := range []string{"messages.0.content", "messages.2.content"} {
		before := gjson.GetBytes(turn2, path).String()
		after := gjson.GetBytes(turn3, path).String()
		if before != after {
			t.Errorf("%s was rewritten between turns: %d -> %d bytes; "+
				"the cached prefix is invalidated from here on",
				path, len(before), len(after))
		}
	}
}

// threeReadTurns is twoReadTurns with a third read of the same file appended —
// the next request in the same session, as the client would send it (the client
// holds the original bytes, so history arrives uncompressed every time).
func threeReadTurns() []byte {
	blob := mustJSON(repeatedRead())
	return []byte(`{"model":"gpt-5","messages":[` +
		`{"role":"user","content":"read the file"},` +
		`{"role":"assistant","tool_calls":[{"id":"t1","function":{"name":"read","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"t1","content":` + blob + `},` +
		`{"role":"user","content":"read it again"},` +
		`{"role":"assistant","tool_calls":[{"id":"t2","function":{"name":"read","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"t2","content":` + blob + `},` +
		`{"role":"user","content":"once more"},` +
		`{"role":"assistant","tool_calls":[{"id":"t3","function":{"name":"read","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"t3","content":` + blob + `}]}`)
}

// TestDedupIsStableAcrossTurns is the cache contract the bug actually broke:
// re-sending the same history must produce the same bytes, or the provider
// re-prices the whole prefix every turn.
func TestDedupIsStableAcrossTurns(t *testing.T) {
	first, _ := Process(twoReadTurns())
	for i := 0; i < 5; i++ {
		again, _ := Process(twoReadTurns())
		if string(again) != string(first) {
			t.Fatalf("run %d differs; prompt-cache prefix unstable", i)
		}
	}
}

// TestDedupIsIdempotent feeds the engine its own output. An already-deduped
// body must come back untouched, otherwise every turn rewrites the prefix.
func TestDedupIsIdempotent(t *testing.T) {
	once, _ := Process(twoReadTurns())
	twice, res := Process(once)
	if string(twice) != string(once) {
		t.Error("second pass rewrote an already-deduped body")
	}
	if res.SavedBytes > 0 {
		t.Errorf("second pass claimed %d more saved bytes from an already-deduped body", res.SavedBytes)
	}
}

// TestDedupNeverRewritesAnthropicHistory covers the same guard on the Anthropic
// path, where the write was gated on `out != ""` alone and rewrote bytes that
// had not changed. Direction is asserted here too: tool_result blocks nest one
// level deeper, so the two transports share a policy but not a code path.
func TestDedupNeverRewritesAnthropicHistory(t *testing.T) {
	blob := mustJSON(repeatedRead())
	body := []byte(`{"model":"claude-3-5-sonnet","messages":[` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":` + blob + `}]},` +
		`{"role":"assistant","content":"reading again"},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":` + blob + `}]}]}`)

	out, _ := Process(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	first := gjson.GetBytes(out, "messages.0.content.0.content").String()
	last := gjson.GetBytes(out, "messages.2.content.0.content").String()
	if first != repeatedRead() {
		t.Errorf("the earlier Anthropic copy was rewritten (%d bytes, original %d)",
			len(first), len(repeatedRead()))
	}
	if len(last) >= len(repeatedRead()) {
		t.Errorf("the later Anthropic copy was not collapsed: %d bytes", len(last))
	}

	again, _ := Process(out)
	if string(again) != string(out) {
		t.Error("Anthropic path is not idempotent; prefix rewrites every turn")
	}
}
