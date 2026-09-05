package engine

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Shape generators shared by the pre-scan neutrality tests and the shape
// benchmarks. They are deliberately in one file: a benchmark that measures a
// different payload than the neutrality test proves nothing about it.
//
// Every generator is deterministic and allocation-stable, and each names what
// the pipeline is supposed to make of it:
//
//	productive — some pass changes the block, so savings must not move
//	dead       — no pass can change it, so the only thing to remove is the cost

// jsonObjectGraph is DEAD work: an object of objects. Tabular lifting needs an
// array of objects and finds none at any depth; structural pruning finds no
// null, no empty container and no metadata key. v0.3.0 still unmarshals the
// whole thing into map[string]any before discovering that.
func jsonObjectGraph(n int) string {
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

// jsonScalarSeries is DEAD work: the arrays hold scalars, so there is no table
// to lift, and nothing in it is prunable.
func jsonScalarSeries(n int) string {
	var b strings.Builder
	b.WriteString(`{"metric":"latency_ms","series":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Itoa(i * 3 % 1_000))
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

// jsonLiftableRows is PRODUCTIVE: a paginated envelope whose array of uniform
// objects lifts into a Markdown table, with the scalar envelope fields printed
// in the headline. This is the shape FR-3 widened reachability to.
func jsonLiftableRows(n int) string {
	var b strings.Builder
	b.WriteString(`{"object":"list","has_more":false,"total_count":` + strconv.Itoa(n) +
		`,"next_cursor":null,"rows":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		id := strconv.Itoa(i)
		b.WriteString(`{"id":"row-` + id + `","name":"widget ` + id +
			`","status":"` + [...]string{"ok", "warn", "stale"}[i%3] +
			`","quantity":` + strconv.Itoa(i*11%97) + `}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// proseBlob is DEAD work: the classifier calls it prose and squoze never
// naive-truncates prose. The gate cannot skip it before Classify runs, so the
// win here is only the passes that follow.
func proseBlob(paras int) string {
	var b strings.Builder
	for i := 0; i < paras; i++ {
		b.WriteString("The gateway resolves an account for the request, applies the routing " +
			"policy, then forwards it upstream while accounting for tokens. Paragraph " +
			strconv.Itoa(i) + " restates that in slightly different words so the text " +
			"stays incompressible by structure rather than by repetition alone.\n\n")
	}
	return b.String()
}

// codeBlob is DEAD work by policy: KindCode is never touched, not even by
// terminal hygiene, because the caller is about to compile or diff it.
func codeBlob(funcs int) string {
	var b strings.Builder
	b.WriteString("package widget\n\nimport \"fmt\"\n\n")
	for i := 0; i < funcs; i++ {
		id := strconv.Itoa(i)
		b.WriteString("func Handle" + id + "(in int) (int, error) {\n" +
			"\tif in < 0 {\n\t\treturn 0, fmt.Errorf(\"negative input %d\", in)\n\t}\n" +
			"\tout := in * " + id + "\n\treturn out, nil\n}\n\n")
	}
	return b.String()
}

// logBlob is PRODUCTIVE: timestamped log lines with ANSI colour, which the
// classifier calls log output and compress.Text squeezes hard.
func logBlob(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		ts := "2026-09-05T10:" + pad2(i/60%60) + ":" + pad2(i%60) + "Z"
		lvl := "\x1b[32mINFO\x1b[0m"
		if i%97 == 0 {
			lvl = "\x1b[31mERROR\x1b[0m"
		}
		b.WriteString(ts + " " + lvl + " request handled account=acct-" +
			strconv.Itoa(i%13) + " upstream=openai latency_ms=" +
			strconv.Itoa(i%400) + " tokens=" + strconv.Itoa(i%2000) + "\r\n")
	}
	return b.String()
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// diffBlob is PRODUCTIVE: a unified diff with long runs of unchanged context,
// which pass 1 compacts while keeping every changed line verbatim.
func diffBlob(files int) string {
	var b strings.Builder
	for f := 0; f < files; f++ {
		name := "internal/pkg" + strconv.Itoa(f) + "/handler.go"
		b.WriteString("diff --git a/" + name + " b/" + name + "\n")
		b.WriteString("--- a/" + name + "\n+++ b/" + name + "\n")
		b.WriteString("@@ -1,40 +1,41 @@\n")
		for i := 0; i < 20; i++ {
			b.WriteString(" unchanged context line " + strconv.Itoa(i) + " with padding\n")
		}
		b.WriteString("-\told := compute(in)\n+\tnew := compute(in, opts)\n")
		for i := 0; i < 20; i++ {
			b.WriteString(" trailing context line " + strconv.Itoa(i) + " with padding\n")
		}
	}
	return b.String()
}

// turnsBody renders an OpenAI chat body of `turns` tool results, asking gen for
// each turn's content so no two blocks are byte-identical — the cross-turn dedup
// pass must not be what a shape measurement is actually measuring.
//
// Variation is the generator's job rather than a prefix this function bolts on,
// because a prefix would change the *shape*: "turn 3\n{...}" no longer starts
// with a brace, so the JSON pass declines it and a fixture meant to exercise
// tabular lifting would quietly measure nothing at all.
func turnsBody(tb testing.TB, turns int, gen func(turn int) string) []byte {
	tb.Helper()
	var b strings.Builder
	b.WriteString(`{"model":"gpt-5","messages":[{"role":"system","content":"sys"}`)
	for i := 0; i < turns; i++ {
		raw, err := json.Marshal(gen(i))
		if err != nil {
			tb.Fatal(err)
		}
		b.WriteString(`,{"role":"user","content":"step ` + strconv.Itoa(i) + `"}`)
		b.WriteString(`,{"role":"tool","content":` + string(raw) + `}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// dupTurnsBody is turnsBody with byte-identical blobs, which is the shape the
// cross-turn dedup pass exists for. AC-2.3 pins that the gate does not disturb
// the savings it finds.
func dupTurnsBody(tb testing.TB, turns int, blob string) []byte {
	tb.Helper()
	raw, err := json.Marshal(blob)
	if err != nil {
		tb.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`{"model":"gpt-5","messages":[{"role":"system","content":"sys"}`)
	for i := 0; i < turns; i++ {
		b.WriteString(`,{"role":"user","content":"re-read the file"}`)
		b.WriteString(`,{"role":"tool","content":` + string(raw) + `}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

type shapeCase struct {
	name string
	body []byte
	// productive says some pass is expected to change this body. Neutrality is
	// asserted for both kinds; this field keeps the corpus honest by letting a
	// test check that the productive half really does produce savings, so a
	// regression cannot turn the whole corpus into no-ops and still pass.
	productive bool
}

// shapeCorpus is the fixture set both the neutrality test and the shape
// benchmarks run over.
func shapeCorpus(tb testing.TB) []shapeCase {
	tb.Helper()
	return []shapeCase{
		{"json_object_graph", turnsBody(tb, 3, func(i int) string { return jsonObjectGraph(400 + i) }), false},
		{"json_scalar_series", turnsBody(tb, 3, func(i int) string { return jsonScalarSeries(2_000 + i) }), false},
		{"json_liftable_rows", turnsBody(tb, 3, func(i int) string { return jsonLiftableRows(300 + i) }), true},
		{"prose", turnsBody(tb, 3, func(i int) string { return proseBlob(120 + i) }), false},
		{"code", turnsBody(tb, 3, func(i int) string { return codeBlob(120 + i) }), false},
		{"logs", turnsBody(tb, 3, func(i int) string { return logBlob(1_200 + i) }), true},
		{"diff", turnsBody(tb, 2, func(i int) string { return diffBlob(6 + i) }), true},
		{"test_output", turnsBody(tb, 2, func(i int) string { return bigTestBlob() + "\n#" + strconv.Itoa(i) }), true},
		{"duplicate_reads", dupTurnsBody(tb, 4, bigTestBlob()), true},
		{"tiny", chatBody("short tool result"), false},
		{"anthropic", anthropicBody(tb, logBlob(400)), true},
	}
}

// anthropicBody renders the Anthropic Messages shape, which travels a different
// scanner (processAnthropicFast) than the OpenAI one.
func anthropicBody(tb testing.TB, blob string) []byte {
	tb.Helper()
	raw, err := json.Marshal(blob)
	if err != nil {
		tb.Fatal(err)
	}
	return []byte(`{"model":"claude-opus-4-5","system":"sys","messages":[` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":` +
		string(raw) + `}]}]}`)
}
