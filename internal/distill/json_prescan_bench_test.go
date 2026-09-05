package distill

import (
	"strconv"
	"strings"
	"testing"
)

// The pre-scan's whole purpose is measured here, at the level where the switch
// exists: distillJSON(s, gate) is DistillJSON with the structural pre-scan on
// or off, so the two arms differ in exactly one decision.
//
// Reproduce:
//
//	docker run --rm -v "$PWD:/w" -w /w golang:1.22 \
//	  go test ./internal/distill/ -run '^$' -bench Gate -benchtime 50x
//
// b.SetBytes carries the payload size, so the report is self-describing: ns/op
// against MB/s says both how long a shape took and how big it was.

// benchObjectGraph is the pathological case: an object of objects, so there is
// no array to lift and nothing to prune. v0.3.0 unmarshals all of it before
// finding that out.
func benchObjectGraph(n int) string {
	var b strings.Builder
	b.WriteString(`{"kind":"graph","nodes":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		id := strconv.Itoa(i)
		b.WriteString(`"n` + id + `":{"label":"service-` + id +
			`","score":` + strconv.Itoa(i*7%991) +
			`,"owner":"team-` + strconv.Itoa(i%17) +
			`","region":"eu-central-` + strconv.Itoa(i%3) + `"}`)
	}
	b.WriteString(`}}`)
	return b.String()
}

// benchSeries is the second dead shape: arrays, but of scalars.
func benchSeries(n int) string {
	var b strings.Builder
	b.WriteString(`{"metric":"latency_ms","series":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Itoa(i * 3 % 1000))
	}
	b.WriteString(`],"labels":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"t` + strconv.Itoa(i) + `"`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// benchLiftable is the productive shape, present so the report shows what the
// gate costs when the answer is yes: one extra structural scan before work that
// was going to happen anyway.
func benchLiftable(n int) string {
	return `{"object":"list","has_more":false,"total_count":` + strconv.Itoa(n) +
		`,"next_cursor":null,"rows":` + rowsArray(n, "row") + `}`
}

func BenchmarkDistillJSONGate(b *testing.B) {
	shapes := []struct {
		name string
		body string
	}{
		{"object_graph_140KB", benchObjectGraph(1_400)},
		{"object_graph_560KB", benchObjectGraph(5_600)},
		{"scalar_series_140KB", benchSeries(9_000)},
		{"liftable_rows_140KB", benchLiftable(1_900)},
	}
	for _, s := range shapes {
		for _, gate := range []bool{true, false} {
			name := s.name + "/prescan=" + strconv.FormatBool(gate)
			body := s.body
			b.Run(name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					out, ok := distillJSON(body, gate)
					benchSink = out
					benchOK = ok
				}
			})
		}
	}
}

var (
	benchSink string
	benchOK   bool
)
