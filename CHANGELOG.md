# Changelog — squoze

Notable changes, newest first. Reconstructed from git history; dates are tag
dates. The public API surface is [`squoze.go`](squoze.go); on the `0.y.z` line
the minor bumps for anything a caller can observe, and breaking changes are
called out explicitly.

## v0.3.0 — 2026-09-05

Contract repair. The benchmark audit in the 2papi gateway
(`2papi/docs/benchmark-audit.md`) ran v0.2.0 through a 15-case corpus and found
that three of the five contracts this README advertises were not actually held,
plus three further defects that surfaced while fixing them. Compression strength
was never the problem — it measured *better* than claimed — so nothing here
trades savings for correctness: median savings on the cases both versions
compress moved 97.06% → 97.02%, and one case went 24.4% → 76.2%.

No public API change: `squoze.NewEngine` and `Apply` keep their signatures. It
is still a minor bump, not a patch: the dedup marker text and direction, the
tabular threshold and the new dedup floor are all observable in the output, and
anything a caller can string-match on is part of the contract.

### Fixed

- **Deterministic column order in tabular lifting.** `colKeys` was built by
  ranging a Go map, so the same input produced up to 4 different table headers
  across 12 fresh engines — which invalidates the provider prompt-cache prefix
  every time a lifted table appears. Column order now follows the order the
  producer used in the document (`sort.Strings` only as a fallback).
- **Source code is passed through untouched.** `router.Kind` declared `KindCode`
  but `Classify` never returned it, so a Go file dense in `FAILED`/`PASSED`
  string literals was elided as if it were test output — 29159 → 4245 bytes of
  syntactically broken source. A column-0 opener (`package`, `func`, `class`,
  `#include`, `export …`) plus marker density now means "file", and the code
  check runs *before* the test-output check: fixture generators and golden files
  embed machine output verbatim, which is precisely why they used to trip it.
- **Panic traces and compiler output are compressed at all.** Neither contains
  `FAIL` or `PASS`, so both used to fall through to `KindProse` and were left
  untouched. `panic:` / `goroutine N [running]:` / `fatal error:` and
  `path/file.go:12:5: message` diagnostics now count as machine output.
- **Cross-turn dedup replacements reach the request body.** The change guard
  compared the deduped value against itself (`out != t.content`, where
  `t.content` had already been rewritten by dedup), so the replacement was
  silently discarded and the earlier turn was resent in full — +24914 bytes on a
  4-turn session. `toolTarget` now keeps the body's original bytes separately.
- **JSON envelope fields survive tabular lifting.** `has_more`, `object`,
  counts and the wrapper key used to disappear with the array, so a model shown
  800 rows had no way to know more pages existed.
- **Failure lines dropped by `MaxKept` are disclosed.** At `MaxKept=50` and 200
  failures, error-line recall was 8% and the marker said nothing about it. It
  now carries `· N more failure lines over cap=50, see local copy`.
- **Truncated table cells are disclosed.** `sanitizeCell` cuts at 80 chars; the
  headline now reports `N cells truncated`, so savings that are really deletion
  are visible as such.

### Changed

- **Dedup direction is inverted, and the marker text changed with it.** v0.2.0
  kept the *latest* copy of a re-read file intact and rewrote the earlier ones.
  That is backwards for prompt caching: the earlier bytes are the ones already
  sent and already cached, so editing them discards the cached prefix from that
  point on. The first copy is now the one that stays; later repeats collapse to

  ```
  [... squoze: identical to the copy in turn N above (B bytes) · ref R ...]
  ```

  replacing `[... squoze: earlier view from turn N ...]`. `N` is now the *first*
  turn rather than the latest, so the marker stops changing as the session grows
  — with the old form, collapsed history was rewritten on every subsequent turn.
  **Anyone string-matching the old marker must update.**
- **Tabular lifting requires ≥35% savings** (was 10%). Turning valid JSON into a
  Markdown table is a type change; 10% did not justify it, and structural
  pruning keeps the value parseable at the margin.
- **Constant columns are hoisted out of the table.** A column whose value repeats
  in every row is stated once in the headline (`· all rows: note=…`) instead of
  800 times. A value that would have to be truncated is not hoisted — a clipped
  value in the headline has nothing to disclose it.
- **Dedup floor of 512 bytes.** Collapsing a 200-byte repeat into a 90-byte
  marker is not a saving, and every such rewrite costs prefix.

### Internal

No behaviour change in this section — same verdicts, same output bytes.

- **The router is now benchmarked directly.** `Classify` runs on every tool block
  *before* any compression, so an untouched blob pays it too, yet
  `internal/router` had no benchmarks at all. Each case pins the `Kind` it
  expects before measuring, because a benchmark that quietly changes branch keeps
  printing a number for the wrong thing — that pin is what found the item below.
  Two readings worth keeping: the sampling cap holds (270 KB and 2700 KB cost the
  same within noise), and `KindCode` is the *cheapest* full path, ~45% under
  letting the same blob fall through to prose.
- **No `json.Valid` call on prefixes that cannot be JSON.** `Classify` validates
  a *truncated* 8 KB prefix, so every JSON tool result larger than that is
  invalid by construction: the parse could only fail, and its cost was a full 8 KB
  scan plus the `json.SyntaxError` the failure heap-allocates and nobody reads —
  the one allocation left anywhere in the router. It now runs only when the
  prefix closes with the bracket matching its opener, which cannot reject
  anything `json.Valid` would have accepted (valid JSON always ends that way).
  Measured: 41–49 µs saved per large JSON object result, 46–61 µs per array, and
  the router is allocation-free on every branch (24 B → 0 B, 1 → 0 allocs,
  p=0.002).

  Note for anyone reading `Kind` in a fork: `KindJSON` is unreachable above 8 KB
  and always has been. It is not observable — both engine call sites treat
  `KindJSON` exactly as `KindProse`, and JSON distillation is offered to the body
  earlier in the pipeline regardless — so making the branch reachable would mean
  parsing JSON in full on the hot path for zero change in output. The
  unreachability stays; only its cost is gone.

### Notes

Requirements, design and per-task acceptance (including the re-run commands for
every number above) live in
[`.kiro/specs/distill-contracts/`](.kiro/specs/distill-contracts/).

## v0.2.0 — 2026-09-03

Squoze v2 — the Streaming Context Distillation Engine. Sub-millisecond pipeline latency (<0.6 ms), Unified Diff distillation (Diff-Squoze), Structural JSON Tabular Lifting (J-Squoze), Cross-Turn Stale Read Deduplication, and support for coding agent user-tool wrappers (`role: "user"`).

### Added

- **Squoze v2 Single-Pass Streaming Scanner** — Single pass `gjson` offset scanner (`processOpenAIChatFast` & `processAnthropicFast`) with zero-copy buffer stitching. Engine latency reduced to `<0.6 ms`.
- **Unified Diff Distillation (`Diff-Squoze`)** — Automatically collapses massive dependency lockfiles (`pnpm-lock.yaml`, `go.sum`, `Cargo.lock`, `package-lock.json`) and minified assets into 1-line semantic summaries while keeping real code modifications 100% untouched.
- **Cross-Turn Stale Read Deduplication** — Tracks multi-turn agent file reads and collapses repetitive historical tool outputs into lightweight back-references (`[... squoze: earlier view from turn N ...]`).
- **Structural JSON Pruning (`J-Squoze`)** — Lifts repeated JSON array records into compact schema-preserved tabular formats.
- **Machine Output Compression in `role: "user"`** — Detects XML wrappers (`<tool_output>`, `<command_output>`, `<file_content>`) and fenced code blocks (````terminal`, ````diff`) sent by agents (Aider, Cursor, Cline, OpenCode) within user messages. Squeezes machine noise while preserving 100% of human instructions.
- ~~**Validated against SWE-bench & Live GitHub Issues** — 100% Pass@1 on
  SWE-bench Verified (`django__django-16595`), TerminalBench v2.1, Aider
  Polyglot, and autonomous contribution to `go-chi/chi` (PR #1171).~~
  **Retracted 2026-09-05.** Those suites graded by substring-matching fixture
  replies and the instance IDs were not upstream SWE-bench instances, so the
  numbers described the harness, not the engine. Nothing was re-scored to
  replace them: what the corpus does measure is in the README, and the audit
  that disproved these is `2papi/docs/benchmark-audit.md`.

## v0.1.3 — 2026-09-02

First release published by CI rather than by hand.

### Added

- **Release automation** — `.goreleaser.yaml` plus a tag-gated release job build
  static binaries for linux/darwin/windows × amd64/arm64 (minus windows-arm64)
  and publish them with checksums on every `v*` tag. Archives carry `README.md`
  and `LICENSE`.
- `squoze help` as an explicit subcommand.

### Fixed

- **`squoze --version` / `-v` / `-version` now work.** They previously fell
  through to usage and exited 2; only the bare `version` subcommand worked. This
  matters because `SECURITY.md` asks vulnerability reporters to run
  `squoze --version` to identify their build.
- **`squoze help` / `--help` / `-h` now print usage to stdout and exit 0**
  instead of stderr with exit 2, so help output can be piped. Bare `squoze` and
  unknown commands still write usage to stderr and exit 2.
- **Unknown commands are named** — `squoze: unknown command "foo"` precedes
  usage, so a typo is distinguishable from a missing feature.
- **Usage listed 4 of 7 subcommands.** `agent`, `harness`, `livecheck` and the
  `squoze oc` shorthand were documented in the README but invisible from the
  binary. Usage now matches the dispatch table, and reserved words resolve
  before agent-alias lookup so an agent cannot shadow `version` or `help`.
- `Version` in `internal/engine` read `0.1.0` while tags had reached `v0.1.2`, so
  `squoze version` and the savings response header under-reported the build.

## v0.1.2 — 2026-09-02

The release that made squoze embeddable from other modules, plus the `oc`
one-command agent lifecycle.

### Added

- **Library facade** (`squoze.go`) — `NewEngine`, `Process`, `Engine`, `Result`,
  `Version`, `DefaultMemoCapacity` re-exported at the module root. `internal/`
  packages are not importable from outside the module, so this is the supported
  embedding contract. The gateway in
  [2papi](https://github.com/Rethinger/2papi) consumes it.
- **`squoze oc` / `sq <agent>`** — one-command agent lifecycle: dynamic
  per-request upstream via a routing header, auto-launch and auto-unwind.
  `--auto` is the default for config agents; `--manual` opts out.
- **Automatic config wiring** (`--auto` / `--unwire`) for opencode and omp, with
  backups of the files it edits.
- **Harness presets** — one-command wiring for claude, openai, gemini, deepseek,
  openrouter and fireworks; agent-level opencode/omp snippets and env-agent wrap
  presets.
- **All providers wired** — both configured providers and catalog defaults; OAuth
  rides the routing header.
- **Per-provider listener ports**, so custom OpenAI-compatible providers work
  around [opencode#5674](https://github.com/sst/opencode/issues/5674).
- **`--log FILE`** — full JSONL request log for proxy and wrap modes.
- **CMD override for config agents**, for headless testing.

### Fixed

- Proxy double-join on the provider prefix.
- Provider targeting (`--provider` plus auto-detect), `baseURL` path
  preservation, and BOM-tolerant config parsing — all found during a live
  owner test.

## v0.1.1 — 2026-08-24

### Added

- **`squoze livecheck`** — provider smoke-test subcommand.
- Context-after rescue, so multi-line upstream errors survive compression
  instead of being truncated mid-message.

## v0.1.0 — 2026-08-24

First tagged release: Apache-2.0 LICENSE, CI workflow (ubuntu + windows),
`.gitignore`.

### Added

- **Content router with head/tail compression** — 98.9% byte reduction on the
  270 KB test blob, with upstream error text preserved.
- **Skeleton** — wire-format detection, fail-open passthrough proxy, savings
  response headers. Fail-open is a design rule: if anything goes wrong the
  original bytes pass through untouched.
- **Cache-guard decision memo** plus the C1–C7 quality-contract suite, including
  turn-prefix stability (compression must not break upstream prompt-cache hits).
- **Model-family profiles** and a content-addressed originals store with marker
  refs and a `retrieve` CLI.
- **Wrap mode** with env injection, an engine-bound proxy, and persisted
  originals wiring.
- **Deterministic quality-fixture eval harness** with benchmarks and an
  external-eval protocol document.

## Pending work

Not a release section, which is why it sorts below the oldest tag.

Pending work is tracked in [docs/UNFINISHED.md](docs/UNFINISHED.md) — notably the
unsigned-binary Windows Defender false positive, the L2 evaluation, and
code signing for released binaries.
