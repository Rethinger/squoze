# Changelog — squoze

Notable changes, newest first. Reconstructed from git history; dates are tag
dates. Versions follow the `v0.1.x` line — the public API surface is
[`squoze.go`](squoze.go), and breaking changes to it are called out explicitly.

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

## Unreleased

Pending work is tracked in [docs/UNFINISHED.md](docs/UNFINISHED.md) — notably the
unsigned-binary Windows Defender false positive, the L2 evaluation, and
code signing for released binaries.
