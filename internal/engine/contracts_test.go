package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Rethinger/squoze/internal/compress"
	"github.com/tidwall/gjson"
)

// This suite pins the squoze quality contracts as executable checks.
// Every transform added later must keep all of these green:
//
//	C1 fail-open        — broken/unknown input passes byte-for-byte
//	C2 never-elide      — failure signal survives every squeeze
//	C3 idempotency      — processing twice equals processing once
//	C4 savings floor    — marginal squeezes are rejected
//	C5 untouched bytes  — non-target regions stay byte-identical
//	C6 valid JSON out   — valid JSON in → valid JSON out
//	C7 turn stability   — unchanged history compresses to identical bytes,
//	                      so multi-turn prefixes keep hitting provider caches

func bigTestBlob() string {
	var b strings.Builder
	b.WriteString("$ go test ./...\n")
	for i := 0; i < 400; i++ {
		if i%50 == 0 {
			b.WriteString("--- FAIL: TestX (0.01s)\n    x_test.go:9: boom\n")
		}
		b.WriteString("ok  verbose test output line with plenty of padding padding padding\n")
	}
	return b.String()
}

func chatBody(toolContent string) []byte {
	raw, _ := json.Marshal(toolContent)
	return []byte(`{"model":"m","messages":[` +
		`{"role":"system","content":"sys prompt"},` +
		`{"role":"user","content":"run tests"},` +
		`{"role":"tool","content":` + string(raw) + `}]}`)
}

func TestContract_FailOpen(t *testing.T) { // C1
	for _, in := range []string{`{oops`, ``, `null`, `[]`, `{"model":m"}`} {
		out, res := Process([]byte(in))
		if string(out) != in || res.SavedBytes != 0 {
			t.Fatalf("C1 violated for %q: out=%q saved=%d", in, out, res.SavedBytes)
		}
	}
}

func TestContract_NeverElide(t *testing.T) { // C2
	body := chatBody(bigTestBlob())
	out, _ := Process(body)
	content := gjson.GetBytes(out, "messages.2.content").String()
	want := strings.Count(bigTestBlob(), "--- FAIL")
	if got := strings.Count(content, "--- FAIL"); got != want {
		t.Fatalf("C2 violated: FAIL lines %d -> %d", want, got)
	}
	if !strings.Contains(content, "boom") {
		t.Fatal("C2 violated: error detail lost")
	}
}

func TestContract_IdempotentAtEngineLevel(t *testing.T) { // C3
	e := NewEngine(DefaultMemoCapacity)
	body := chatBody(bigTestBlob())
	out1, _ := e.Apply(body)
	out2, _ := e.Apply(out1)
	if !bytes.Equal(out1, out2) {
		t.Fatalf("C3 violated: %d -> %d bytes on second pass", len(out1), len(out2))
	}
}

func TestContract_UntouchedBytes(t *testing.T) { // C5
	body := chatBody(bigTestBlob())
	out, _ := Process(body)
	if gjson.GetBytes(out, "model").String() != "m" ||
		gjson.Get(string(out), "messages.0.content").String() != "sys prompt" ||
		gjson.Get(string(out), "messages.1.content").String() != "run tests" {
		t.Fatalf("C5 violated: untouched region changed:\n%s", out)
	}
}

func TestContract_ValidJSONOut(t *testing.T) { // C6
	out, _ := Process(chatBody(bigTestBlob()))
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("C6 violated: %v", err)
	}
}

func TestContract_TurnPrefixStability(t *testing.T) { // C7 — the cache contract
	// Turn 2 is built by string surgery on turn 1's exact bytes, inserting a
	// new message before the closing bracket: shared history = byte-identical input.
	const jsonTail = `]}`
	turn1 := chatBody(bigTestBlob())
	turn2 := append([]byte{}, turn1[:len(turn1)-len(jsonTail)]...)
	turn2 = append(turn2, []byte(`,{"role":"user","content":"and now fix it"}`)...)
	turn2 = append(turn2, jsonTail...)

	out1, r1 := Process(turn1)
	out2, r2 := Process(turn2)

	if r1.BlocksSqueezed != 1 || r2.BlocksSqueezed < 1 {
		t.Fatalf("fixture broken: blocks t1=%d t2=%d", r1.BlocksSqueezed, r2.BlocksSqueezed)
	}

	shared1 := out1[:len(out1)-len(jsonTail)]
	if !bytes.HasPrefix(out2, shared1) {
		bad := firstDiff(shared1, out2)
		t.Fatalf("C7 violated at byte %d: provider prefix cache would miss.\n want %.80q\n got  %.80q",
			bad, shared1[maxInt(0, bad-40):bad+40], out2[maxInt(0, bad-40):bad+40])
	}
}

func TestContract_MemoPinsDecisions(t *testing.T) { // cache-guard mechanics
	e := NewEngine(DefaultMemoCapacity)
	body := chatBody(bigTestBlob())
	out1, r1 := e.Apply(body)
	out2, r2 := e.Apply(body)
	if !bytes.Equal(out1, out2) {
		t.Fatal("same engine must produce identical output for identical input")
	}
	if r2.MemoHits == 0 || r1.MemoHits != 0 {
		t.Fatalf("memo accounting wrong: pass1=%d pass2=%d", r1.MemoHits, r2.MemoHits)
	}
}

func TestSavingsFloorStillEnforced(t *testing.T) { // C4 (compress-level covered too)
	longProse := strings.Repeat("Careful prose about deployment plans and rollback strategy. ", 60)
	out, res := Process(chatBody(longProse))
	if res.BlocksSqueezed != 0 || res.SavedBytes != 0 {
		t.Fatalf("C4/C-routing violated: %+v", res)
	}
	_ = out
}

func TestContract_ReversibleViaMarkerRef(t *testing.T) { // C8 — the reversibility contract
	e := NewEngine(DefaultMemoCapacity) // memory-only originals
	body := chatBody(bigTestBlob())
	out, res := e.Apply(body)
	if res.BlocksSqueezed != 1 {
		t.Fatalf("fixture broken: %+v", res)
	}

	content := gjson.GetBytes(out, "messages.2.content").String()
	i := strings.Index(content, "kept locally as ")
	if i < 0 {
		t.Fatalf("marker has no ref: %.200q", content)
	}
	rest := content[i+len("kept locally as "):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 || end < compress.RefHexLen {
		t.Fatalf("ref truncated in marker: %.80q", rest)
	}
	ref := rest[:end]

	orig, err := e.orig.Resolve(ref)
	if err != nil {
		t.Fatalf("C8 violated: ref %q unresolvable: %v", ref, err)
	}
	if !strings.Contains(orig, "--- FAIL: TestX (0.01s)") {
		t.Fatal("C8 violated: resolved original is not the full text")
	}
}

func TestContract_ProfileAffectsAggressiveness(t *testing.T) {
	medium := strings.Repeat("tests/test_pad.py PASSED verbose machine output line\n", 40) // ~2.3KB
	mkBody := func(model string) []byte {
		raw, _ := json.Marshal(medium)
		return []byte(`{"model":"` + model + `","messages":[{"role":"tool","content":` + string(raw) + `}]}`)
	}
	claudeOut, claudeRes := Process(mkBody("claude-sonnet-4"))
	deepOut, deepRes := Process(mkBody("deepseek-chat"))

	if claudeRes.BlocksSqueezed != 0 {
		t.Fatalf("claude preset must skip ~2.7KB blob, squeezed %d", claudeRes.BlocksSqueezed)
	}
	if deepRes.BlocksSqueezed == 0 || len(deepOut) >= len(claudeOut) {
		t.Fatalf("deepseek preset must squeeze what claude skips: %+v", deepRes)
	}
}

func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n // b is shorter or equal up to n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
