package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSavingsSmoke prints a deterministic before/after measurement on a
// realistic 4000-line go-test blob. Run with -v to read the numbers.
func TestSavingsSmoke(t *testing.T) {
	var b strings.Builder
	b.WriteString("$ go test ./...\n")
	for i := 0; i < 4000; i++ {
		if i%500 == 0 {
			b.WriteString("--- FAIL: TestPayment (0.01s)\n    payment_test.go:44: balance mismatch\n")
		}
		b.WriteString("ok  verbose test output line with plenty of padding padding padding\n")
	}
	raw, _ := json.Marshal(b.String())
	body := []byte(`{"model":"m","messages":[` +
		`{"role":"user","content":"run tests"},` +
		`{"role":"tool","content":` + string(raw) + `}]}`)

	out, res := Process(body)
	if res.SavedBytes <= 0 {
		t.Fatal("expected savings")
	}
	if !json.Valid(out) {
		t.Fatal("invalid output JSON")
	}
	t.Logf("original=%d sent=%d saved=%d (%.1f%%) blocks=%d",
		res.OriginalBytes, res.SentBytes, res.SavedBytes,
		100*float64(res.SavedBytes)/float64(res.OriginalBytes), res.BlocksSqueezed)
}
