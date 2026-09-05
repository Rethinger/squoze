package engine

import (
	"strconv"
	"testing"
)

// Shape benchmarks: the permanent replacement for the throwaway probe that
// found the latency defect. Each shape is measured twice, with the structural
// pre-scan live and with it disabled, so the ratio between the two arms is the
// gate's effect on that shape and nothing else — same fixture, same engine,
// same corpus the neutrality test proves the arms agree on.
//
// Reproduce:
//
//	docker run --rm -v "$PWD:/w" -w /w golang:1.22 \
//	  go test ./internal/engine/ -run '^$' -bench 'Shapes|Turns' -benchtime 20x
//
// A fresh Engine per iteration is deliberate. The decision memo is keyed by
// content, so reusing one engine would serve every iteration after the first
// from the memo and measure a map lookup instead of the pipeline. Both arms pay
// the same construction cost, so the comparison stays fair.
//
// What this pair does NOT measure is the JSON pass, and the reason is worth
// stating where the numbers are read: prescanOff switches off the engine-level
// gate only, while the public distill.DistillJSON gates unconditionally. So both
// arms here decline dead JSON inside the JSON pass, and the delta shown is the
// value of stopping *earlier* — before Classify, the diff checks and
// compress.Text. The JSON pass's own before/after is
// BenchmarkDistillJSONGate in internal/distill, where the switch is reachable.
// benchMemoCapacity keeps engine construction out of the measurement. Memo
// construction pre-sizes its map to the capacity, so DefaultMemoCapacity (4096)
// costs ~186 us and ~370 KB per iteration — which on a small shape is most of
// what the benchmark would otherwise be reporting. Capacity cannot change any
// verdict here: every shape's content is distinct, so nothing is ever served
// from the memo and only its allocation cost differs.
const benchMemoCapacity = 64

func BenchmarkShapes(b *testing.B) {
	for _, c := range shapeCorpus(b) {
		for _, gate := range []bool{true, false} {
			name := c.name + "/prescan=" + strconv.FormatBool(gate)
			body := c.body
			b.Run(name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					eng := NewEngine(benchMemoCapacity)
					eng.prescanOff = !gate
					out, _ := eng.Apply(append([]byte(nil), body...))
					sink = out
				}
			})
		}
	}
}

// BenchmarkTurns measures the same two arms on agent-shaped sessions: many
// turns of one payload kind, which is where a per-block cost multiplies. 60 and
// 300 turns bracket a long coding session.
func BenchmarkTurns(b *testing.B) {
	kinds := []struct {
		name string
		gen  func(int) string
	}{
		{"json_object_graph", func(i int) string { return jsonObjectGraph(60 + i) }},
		{"json_liftable_rows", func(i int) string { return jsonLiftableRows(60 + i) }},
		{"logs", func(i int) string { return logBlob(200 + i) }},
		{"prose", func(i int) string { return proseBlob(20 + i) }},
	}
	for _, turns := range []int{60, 300} {
		for _, k := range kinds {
			body := turnsBody(b, turns, k.gen)
			for _, gate := range []bool{true, false} {
				name := k.name + "/turns=" + strconv.Itoa(turns) + "/prescan=" + strconv.FormatBool(gate)
				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(len(body)))
					for i := 0; i < b.N; i++ {
						eng := NewEngine(benchMemoCapacity)
						eng.prescanOff = !gate
						out, _ := eng.Apply(append([]byte(nil), body...))
						sink = out
					}
				})
			}
		}
	}
}
