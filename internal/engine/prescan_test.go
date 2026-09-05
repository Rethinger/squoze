package engine

import (
	"bytes"
	"testing"

	"github.com/Rethinger/squoze/internal/wire"
)

func freshPair() (gated, ungated *Engine) {
	gated = NewEngine(DefaultMemoCapacity)
	ungated = NewEngine(DefaultMemoCapacity)
	ungated.prescanOff = true
	return gated, ungated
}

func cp(b []byte) []byte { return append([]byte(nil), b...) }

// TestPrescanIsOutputNeutral is AC-2.1, the criterion the whole latency guard
// stands on: the gate may remove work, never savings. Two engines process the
// same corpus, one with canDistill live and one with it disabled, and every
// byte of output plus every reported count has to match. If this test is ever
// weakened, the gate stops being a performance change and becomes a behaviour
// change.
func TestPrescanIsOutputNeutral(t *testing.T) {
	for _, c := range shapeCorpus(t) {
		gated, ungated := freshPair()
		outGate, resGate := gated.Apply(cp(c.body))
		outNo, resNo := ungated.Apply(cp(c.body))

		if !bytes.Equal(outGate, outNo) {
			t.Errorf("%s: output differs with the pre-scan (%d bytes) and without it (%d bytes)",
				c.name, len(outGate), len(outNo))
			continue
		}
		if resGate.BlocksSqueezed != resNo.BlocksSqueezed || resGate.SavedBytes != resNo.SavedBytes {
			t.Errorf("%s: accounting differs — gated %d blocks/%d saved, ungated %d blocks/%d saved",
				c.name, resGate.BlocksSqueezed, resGate.SavedBytes,
				resNo.BlocksSqueezed, resNo.SavedBytes)
		}
	}
}

// TestCorpusStillProduces keeps the neutrality test from passing vacuously: if
// a regression turned every shape into a no-op, equality would still hold, so
// the shapes marked productive must actually save bytes and the ones marked
// dead must actually save none.
func TestCorpusStillProduces(t *testing.T) {
	for _, c := range shapeCorpus(t) {
		eng := NewEngine(DefaultMemoCapacity)
		_, res := eng.Apply(cp(c.body))
		switch {
		case c.productive && res.SavedBytes <= 0:
			t.Errorf("%s: marked productive but saved %d bytes", c.name, res.SavedBytes)
		case !c.productive && res.SavedBytes != 0:
			t.Errorf("%s: marked dead but saved %d bytes — reclassify it or the "+
				"gate is being credited for work that does exist", c.name, res.SavedBytes)
		}
	}
}

// TestPrescanRejectsDeadWork is the other half of non-vacuity: the gate has to
// actually reject the expensive shapes it was written for. A canDistill that
// returned true unconditionally would satisfy neutrality perfectly and save
// nothing at all.
func TestPrescanRejectsDeadWork(t *testing.T) {
	dead := map[string]string{
		"object_graph":  jsonObjectGraph(400),
		"scalar_series": jsonScalarSeries(2_000),
		"prose":         proseBlob(120),
	}
	for name, blob := range dead {
		if canDistill(blob) {
			t.Errorf("%s: gate admitted a block no pass can change", name)
		}
	}
	live := map[string]string{
		"liftable_rows": jsonLiftableRows(300),
		"logs":          logBlob(1_200),
		"diff":          diffBlob(6),
		"test_output":   bigTestBlob(),
	}
	for name, blob := range live {
		if !canDistill(blob) {
			t.Errorf("%s: gate rejected a block that does compress", name)
		}
	}
}

// TestZeroLimitsMatchDefaultEngine is AC-1.1: adding Limits to the API may not
// change what an embedder who never mentions Limits observes.
func TestZeroLimitsMatchDefaultEngine(t *testing.T) {
	for _, c := range shapeCorpus(t) {
		plain := NewEngine(DefaultMemoCapacity)
		zero := NewEngineWithLimits(DefaultMemoCapacity, Limits{})
		outPlain, resPlain := plain.Apply(cp(c.body))
		outZero, resZero := zero.Apply(cp(c.body))
		if !bytes.Equal(outPlain, outZero) {
			t.Errorf("%s: zero Limits changed the output (%d vs %d bytes)",
				c.name, len(outPlain), len(outZero))
		}
		if resZero.Skipped || resZero.SkipReason != "" {
			t.Errorf("%s: zero Limits reported a skip: %q", c.name, resZero.SkipReason)
		}
		if resPlain.SavedBytes != resZero.SavedBytes {
			t.Errorf("%s: zero Limits changed savings (%d vs %d)",
				c.name, resPlain.SavedBytes, resZero.SavedBytes)
		}
	}
}

// TestBodyCapSkipsWithoutLooking is AC-1.2. The cap has to return the input
// byte for byte, report why, and — the part that makes it a latency guard
// rather than a savings switch — not even sniff the wire format, since format
// detection walks the whole body.
func TestBodyCapSkipsWithoutLooking(t *testing.T) {
	body := turnsBody(t, 3, func(i int) string { return logBlob(1_200 + i) })
	eng := NewEngineWithLimits(DefaultMemoCapacity, Limits{MaxBodyBytes: 1_024})
	if len(body) <= 1_024 {
		t.Fatalf("fixture too small to exercise the cap: %d bytes", len(body))
	}

	out, res := eng.Apply(cp(body))
	if !bytes.Equal(out, body) {
		t.Errorf("capped request was modified: %d in, %d out", len(body), len(out))
	}
	if !res.Skipped || res.SkipReason != SkipBodyTooLarge {
		t.Errorf("Skipped=%v SkipReason=%q; want true, %q", res.Skipped, res.SkipReason, SkipBodyTooLarge)
	}
	if res.BlocksSqueezed != 0 || res.SavedBytes != 0 {
		t.Errorf("capped request reported work: %d blocks, %d saved", res.BlocksSqueezed, res.SavedBytes)
	}
	if res.Format != wire.FormatUnknown {
		t.Errorf("Format = %v; the cap promises not to parse, so it cannot know the format", res.Format)
	}
	if got := eng.Limits().MaxBodyBytes; got != 1_024 {
		t.Errorf("Limits() = %d; want the configured 1024", got)
	}

	// Just under the cap the same engine must behave exactly like an unbounded
	// one, so the cap is a threshold and not a mode.
	small := chatBody(bigTestBlob())
	if len(small) >= 1_024 {
		small = chatBody("short tool result")
	}
	outSmall, resSmall := eng.Apply(cp(small))
	plainOut, plainRes := NewEngine(DefaultMemoCapacity).Apply(cp(small))
	if !bytes.Equal(outSmall, plainOut) || resSmall.SavedBytes != plainRes.SavedBytes {
		t.Errorf("under the cap the bounded engine diverged: %d/%d bytes, %d/%d saved",
			len(outSmall), len(plainOut), resSmall.SavedBytes, plainRes.SavedBytes)
	}
	if resSmall.Skipped {
		t.Error("under the cap the request was reported as skipped")
	}
}

// TestBlockCapLeavesBigBlockAlone pins the per-block bound: one pathological
// tool_result stops consuming the request's latency budget, and the rest of the
// messages are still optimized. Unlike the body cap this is silent — it is a
// per-block decision and Result reports per-request facts.
func TestBlockCapLeavesBigBlockAlone(t *testing.T) {
	huge := bigTestBlob() // ~27 KB of squeezable test output
	small := logBlob(400) // ~30 KB of squeezable log output
	body := turnsBody(t, 1, func(int) string { return huge })
	mixed := turnsBody(t, 1, func(int) string { return small })

	bounded := NewEngineWithLimits(DefaultMemoCapacity, Limits{MaxBlockBytes: 1_024})
	_, resHuge := bounded.Apply(cp(body))
	if resHuge.BlocksSqueezed != 0 || resHuge.SavedBytes != 0 {
		t.Errorf("block over the cap was still squeezed: %d blocks, %d saved",
			resHuge.BlocksSqueezed, resHuge.SavedBytes)
	}
	if resHuge.Skipped {
		t.Error("a per-block cap must not report a request-level skip")
	}

	// And an unbounded engine on the same body does find savings, so the
	// assertion above is about the cap and not about the fixture.
	_, resRef := NewEngine(DefaultMemoCapacity).Apply(cp(body))
	if resRef.SavedBytes <= 0 {
		t.Fatalf("fixture is not squeezable at all: %d saved", resRef.SavedBytes)
	}

	generous := NewEngineWithLimits(DefaultMemoCapacity, Limits{MaxBlockBytes: 1 << 20})
	_, resMixed := generous.Apply(cp(mixed))
	if resMixed.SavedBytes <= 0 {
		t.Errorf("a cap above every block size changed the outcome: %d saved", resMixed.SavedBytes)
	}
}

// TestMinBlockRaisesFloor pins the lower bound, and that it can only be raised:
// a value under the built-in 64-byte floor is ignored rather than obeyed,
// because below that no pass can save more than its own marker costs.
func TestMinBlockRaisesFloor(t *testing.T) {
	body := turnsBody(t, 1, func(int) string { return bigTestBlob() })

	raised := NewEngineWithLimits(DefaultMemoCapacity, Limits{MinBlockBytes: 1 << 20})
	if _, res := raised.Apply(cp(body)); res.SavedBytes != 0 {
		t.Errorf("floor above the block size still squeezed it: %d saved", res.SavedBytes)
	}

	lowered := NewEngineWithLimits(DefaultMemoCapacity, Limits{MinBlockBytes: 1})
	outLow, resLow := lowered.Apply(cp(body))
	outPlain, resPlain := NewEngine(DefaultMemoCapacity).Apply(cp(body))
	if !bytes.Equal(outLow, outPlain) || resLow.SavedBytes != resPlain.SavedBytes {
		t.Errorf("a floor below the built-in one changed behaviour: %d/%d saved",
			resLow.SavedBytes, resPlain.SavedBytes)
	}
	if got := lowered.minBlock(); got != defaultMinBlockBytes {
		t.Errorf("minBlock() = %d; want the built-in %d", got, defaultMinBlockBytes)
	}
}

// TestDuplicateReadsKeepSavings is AC-2.3: the cross-turn dedup pass runs
// before the gate, so a session that re-reads the same file keeps its very
// large savings. The 96% floor is the number the spec commits to.
func TestDuplicateReadsKeepSavings(t *testing.T) {
	body := dupTurnsBody(t, 4, bigTestBlob())
	_, res := NewEngine(DefaultMemoCapacity).Apply(cp(body))
	ratio := float64(res.SavedBytes) / float64(res.OriginalBytes)
	if ratio < 0.96 {
		t.Errorf("dedup savings = %.1f%% (%d of %d bytes); want >= 96%%",
			100*ratio, res.SavedBytes, res.OriginalBytes)
	}
}
