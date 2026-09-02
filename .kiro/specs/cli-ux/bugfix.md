# Bugfix Spec — squoze CLI usability

Status: in progress · Created 2026-09-02

## Context

`squoze` dispatches seven subcommands plus top-level agent aliases, but the CLI
surface contradicts both its own README and the conventions its sibling project
already follows. A user who installs squoze and types `squoze --version` or
`squoze help` is told nothing useful and gets a failure exit code.

Reference implementation for the intended behaviour is 2papi's own gateway CLI
(`cmd/gateway/main.go:55`), which accepts `version`, `-version` and `--version`.

## Current behavior

Measured on a build of `039e6b3` + the `Version` fix (`cmd/squoze/main.go`):

| Invocation | Exit | Output |
|---|---|---|
| `squoze version` | 0 | `squoze v0.1.2` ✅ |
| `squoze --version` | 2 | usage on stderr ❌ |
| `squoze -v` | 2 | usage on stderr ❌ |
| `squoze help` | 2 | usage on stderr ❌ |
| `squoze --help` / `-h` | 2 | usage on stderr ❌ |
| `squoze bogus-command` | 2 | usage, with no indication the command was unknown ❌ |
| `squoze` (no args) | 2 | usage on stderr (acceptable) |

Usage text (`cmd/squoze/main.go:143-152`) advertises four subcommands —
`proxy`, `wrap`, `retrieve`, `version` — while `main()` dispatches seven:
`proxy`, `wrap`, `version`, `agent`, `harness`, `livecheck`, `retrieve`, plus
top-level agent aliases resolved through `harness.LookupAgent` (so `squoze oc`
≡ `squoze agent opencode`).

So `agent`, `harness`, `livecheck` and the `oc` shorthand — all documented in
README.md — are undiscoverable from the binary itself.

The package doc comment (`cmd/squoze/main.go:1-5`) lists only
`proxy` / `wrap` / `version`.

`SECURITY.md` (added in `614e0ce`) instructs reporters to run `squoze --version`
to identify their build — an invocation that does not work.

## Root cause

`main()` is a single `switch os.Args[1]` with no flag-style aliases and no
`help` case, so every unrecognised token — including `--version`, `-h` and
`help` — falls through to `default:` → `usage(); os.Exit(2)`. `usage()` is a
hand-maintained string literal that was never updated as `agent`, `harness` and
`livecheck` were added (v0.1.1–v0.1.2), so it drifted out of sync with the
dispatch table it documents.

## Expected behavior

| Invocation | Exit | Destination | Output |
|---|---|---|---|
| `squoze version`, `-v`, `-version`, `--version` | 0 | stdout | `squoze v<Version>` |
| `squoze help`, `-h`, `--help` | 0 | stdout | full usage |
| `squoze` (no args) | 2 | stderr | full usage |
| `squoze bogus` | 2 | stderr | `squoze: unknown command "bogus"` then full usage |

Full usage lists every dispatched subcommand — `proxy`, `wrap`, `agent`,
`harness`, `livecheck`, `retrieve`, `version`, `help` — and mentions that agent
names work as top-level aliases (`squoze oc`).

## Unchanged behavior

Explicitly out of scope; these must keep working exactly as they do now:

- The compression engine. No change to `internal/engine`, output bytes,
  determinism, `mustKeep` handling or savings headers.
- Every subcommand's own flags and semantics: `proxy --port/--upstream/
  --origins-dir/--log`, `wrap --upstream/--listen/--origins-dir/--log`,
  `retrieve <ref> --home`, and the `agent`/`harness`/`livecheck` flag sets.
- Top-level agent alias resolution (`squoze oc --help` already exits 0).
- Existing exit codes for real errors: missing `--upstream` stays 2, runtime
  failures stay 1.
- Zero third-party dependencies — no CLI framework may be introduced.

## Regressions to check after the fix

1. `squoze oc` and `squoze agent opencode` still resolve identically, and an
   agent named like a flag is not shadowed by the new alias handling.
2. `squoze version` output format is byte-identical (`squoze v0.1.2`), since
   scripts may parse it.
3. Bare `squoze` still exits 2 — help-on-no-args must not become exit 0, or
   shell callers lose their error signal.
4. `go vet ./...`, `gofmt -l .` clean; `go test -race ./...` green on ubuntu and
   windows (CI matrix).
5. `SECURITY.md`'s `squoze --version` instruction becomes true.

## Related

- [../release-automation/requirements.md](../release-automation/requirements.md)
  — the v0.1.2 tag has no published release, so the version this spec makes
  discoverable is also not downloadable.
