package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func benchBlob(lines int) string {
	var b strings.Builder
	b.WriteString("$ go test ./...\n")
	for i := 0; i < lines; i++ {
		if i%100 == 0 {
			b.WriteString("--- FAIL: TestBench (0.01s)\n    b_test.go:1: boom\n")
		}
		b.WriteString("ok  verbose benchmark output line with steady padding padding\n")
	}
	return b.String()
}

func benchBody(tb testing.TB, lines int) []byte {
	tb.Helper()
	raw, err := json.Marshal(benchBlob(lines))
	if err != nil {
		tb.Fatal(err)
	}
	return []byte(`{"model":"gpt-5","messages":[` +
		`{"role":"user","content":"x"},{"role":"tool","content":` + string(raw) + `}]}`)
}

func BenchmarkApply10KB(b *testing.B) {
	body := benchBody(b, 150)
	e := NewEngine(DefaultMemoCapacity)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.Apply(body)
	}
}

func BenchmarkApply270KB(b *testing.B) {
	body := benchBody(b, 4000)
	e := NewEngine(DefaultMemoCapacity)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.Apply(body)
	}
}

// BenchmarkMemoHit measures the cache-guard fast path: identical blob
// re-served from the decision memo without recompression.
func BenchmarkMemoHit(b *testing.B) {
	body := benchBody(b, 4000)
	e := NewEngine(DefaultMemoCapacity)
	e.Apply(body) // warm the memo
	out := body
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, _ = e.Apply(body)
	}
	_ = out
}

func BenchmarkApplyMultiMessage40(b *testing.B) {
	var msgs []string
	raw, _ := json.Marshal(benchBlob(100))
	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			msgs = append(msgs, `{"role":"user","content":"run tests"}`)
		} else {
			msgs = append(msgs, `{"role":"tool","content":`+string(raw)+`}`)
		}
	}
	body := []byte(`{"model":"gpt-5","messages":[` + strings.Join(msgs, ",") + `]}`)
	e := NewEngine(DefaultMemoCapacity)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Apply(body)
	}
}

var sink []byte

func BenchmarkPassthroughUnknownFormat(b *testing.B) {
	body := []byte(`{"weird":` + strings.Repeat("x,", 5000) + `"end":true}`)
	e := NewEngine(DefaultMemoCapacity)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink, _ = e.Apply(body)
	}
}

