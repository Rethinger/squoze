# squoze

> Your context, squoze.

[![ci](https://github.com/Rethinger/squoze/actions/workflows/ci.yml/badge.svg)](https://github.com/Rethinger/squoze/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Rethinger/squoze.svg)](https://pkg.go.dev/github.com/Rethinger/squoze)
[![Go version](https://img.shields.io/github/go-mod/go-version/Rethinger/squoze)](go.mod)
[![Version](https://img.shields.io/github/v/tag/Rethinger/squoze?label=version&sort=semver)](https://github.com/Rethinger/squoze/tags)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Dependencies](https://img.shields.io/badge/dependencies-zero-brightgreen)](go.mod)

**Universal, deterministic LLM context optimizer.** One static binary that sits
between your AI agent and any OpenAI/Anthropic-compatible provider and shrinks
request bodies — without losing information the model needs, and without
breaking provider prompt caches.

squoze detects the wire format, classifies every content block and squeezes
verbose machine output (test runs, logs, CLI progress spam) with head/tail
elision that never drops error lines. Prose, source code and JSON take
different paths. Compression presets adapt per model family, and every elision
is locally reversible by ref.

## Status

`v0.2.0` is the current tag. `main` additionally carries the contract repairs
below, which are **not in any tag yet**.

An external audit of v0.2.0 found that three of the contracts this README
states were not in fact held: table column order was nondeterministic, source
files could be elided as machine output, and already-sent history was rewritten
on a later turn. All three are fixed on `main` and now pinned by tests, with
savings unchanged — median 97.06% → 97.02% across the cases both versions
compress. **The cross-turn dedup marker text changed**; if you match on it, read
[CHANGELOG.md](CHANGELOG.md) before upgrading. Full detail, including how to
re-run each number: [`.kiro/specs/distill-contracts/`](.kiro/specs/distill-contracts/).

## Why

Agents waste most of their token budget on noise: repeated file reads, verbose
CLI output, stale tool results. squoze removes the noise and keeps the signal —
deterministically, under contracts that are tested rather than asserted:

- **Fail-open** — any error means the request goes through untouched.
- **Never-elide** — errors, failures and stack traces are never compressed away.
  Where a cap does force a drop, the marker says how many lines it dropped: a
  silent loss is worse than a disclosed one.
- **Cache-safe** — identical bytes always produce identical output, and bytes
  already sent to the provider are never rewritten on a later turn. Both matter
  for the same reason: a conversation reaches the provider as a growing prefix,
  so one changed byte discards the cache from that position on (a 90% discount
  is worth more than a 20% squeeze).
- **Source-safe** — prose, source code and JSON are classified and routed, not
  mutated. A file you are about to compile or diff comes out byte-identical.
- **Disclosed** — every lossy step reports itself in the marker it leaves:
  dropped failure lines, truncated table cells, hoisted constant columns.
- **Reversible** — originals stay local; the model can pull them back on demand.

## Measured

Two levels, defined in [docs/eval-protocol.md](docs/eval-protocol.md).

**Level 1 — deterministic fixtures, every commit.** `go test ./internal/eval -v`:

| Fixture | Bytes | Saved | Contract |
|---|---:|---:|---|
| `go_test_600` | 39 156 → 2 983 | 92.4% | every `FAIL` line kept |
| `pytest_verbose` | 24 385 → 3 174 | 87.0% | every `FAILED` / `E ` line kept |
| `server_logs_errors` | 30 111 → 1 756 | 94.2% | every `ERROR` line kept |

A violated contract is a red test, not a footnote.

**A 15-case corpus lives in the consumer repo** and grades needle recall,
idempotency, determinism and cross-turn prefix stability as well as savings.
Head of `main`, three runs of the corpus:

```
15 cases · 14 pass · 0 fail · 1 known-limit
compression fired on 9/15 · median savings when it fired: 97.02%
worst-case p95 engine latency: 5.6-6.5 ms across the three runs
needle recall 100% (except the disclosed MaxKept known-limit)
prefix-stable on every turn pair
```

The same corpus on `v0.2.0` scores 11 pass · 3 fail: the three failures are the
contract bugs listed above. One repeat spiked to 14.9 ms on a single case under
host contention from parallel container runs; it is measurement noise, not a
second code path, and it is left in the reports rather than dropped.

```sh
git clone https://github.com/Rethinger/2papi && cd 2papi
go run ./test/squozebench          # writes test/results/squoze_quality_report.json
cat test/squozebench/repro/README.md   # A/B against a released squoze
```

**Not measured: answer quality against a live model.** Level 2 of the protocol
(LoCoMo, RULER, BFCL v3 through a real provider, gate Δaccuracy ≤ 2 pp) is
specified but has not been run. Until it is, squoze makes no claim about task
success rate, Pass@1 or benchmark scores — earlier revisions of this README
quoted such figures from fixtures that could not support them, and that was
wrong. Savings, latency and the contracts above are what the numbers here cover.

## Install

```sh
go install github.com/Rethinger/squoze/cmd/squoze@latest
```

No prebuilt binaries yet — `go install` or `go build` from source. On Windows a
freshly built binary may trip a Defender false positive (ThreatID 251873); add
an exclusion or sign it locally.

## Usage

```sh
# Drop-in proxy for any OpenAI/Anthropic-compatible client
squoze proxy --port 8787 --upstream https://api.anthropic.com
ANTHROPIC_BASE_URL=http://localhost:8787 claude

# Or zero-config: wrap the agent, env injection does the rest
squoze wrap --upstream https://api.anthropic.com claude

# Resolve an elision marker ref back to the full original text
squoze retrieve a3f9c2e1b4d0

# Check savings against a live provider, or patch opencode's provider list
squoze livecheck
squoze oc
```

Every response carries `X-Squoze-Original-Bytes`, `X-Squoze-Sent-Bytes`,
`X-Squoze-Format` and `X-Squoze-Upstream`.

## Use as a library

The root package is a thin facade over the engine — one instance per process
keeps the decision memo warm, which is what makes repeat turns byte-identical:

```go
import "github.com/Rethinger/squoze"

eng := squoze.NewEngine(squoze.DefaultMemoCapacity)
out, res := eng.Apply(requestBody)
```

This is how [2papi](https://github.com/Rethinger/2papi) embeds it as an
optimization mode.

## Build

```sh
go build ./...
go test ./...                # unit tests + quality contracts
go test ./internal/eval -v   # the was → is savings table above
```

Apache-2.0. Pending work is tracked honestly in
[docs/UNFINISHED.md](docs/UNFINISHED.md); released changes in
[CHANGELOG.md](CHANGELOG.md).
