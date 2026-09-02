# Contributing

Thanks for helping make squoze better. The short version: **zero dependencies
is a hard rule**, compression must stay **deterministic and reversible**, and
`mustKeep` regions are never touched.

## Non-negotiables

These are the properties users rely on. A change that breaks one is a bug, even
if it improves the compression ratio:

1. **Zero third-party dependencies.** `go.mod` has no `require` block beyond the
   standard library, and it stays that way. If you need a helper, write it.
2. **Determinism.** The same input bytes must always produce the same output
   bytes. No map iteration order leaking into output, no time-dependent or
   random behaviour.
3. **Reversibility guarantees.** Whatever the mode, the documented `mustKeep`
   regions survive verbatim. Compression is lossy by design *outside* those
   regions only.
4. **No network calls in the engine.** The optimizer is a pure function over
   bytes. Only the `livecheck` / `oc` subcommands may talk to the outside world.

## Repository layout

- `cmd/squoze/` — CLI entry point (`squoze`, plus the `livecheck` and `oc`
  subcommands).
- `internal/engine/` — the optimizer itself: head/tail compression, memoization,
  `mustKeep` handling. `Version` lives here.
- `squoze.go` — the public library facade re-exported for embedders (the
  gateway in [2papi](https://github.com/Rethinger/2papi) consumes this; the
  `internal/` path is not importable from outside the module).
- `testdata/` — fixtures used for the byte-savings numbers quoted in the README.

## Local development

```sh
go test ./...          # full suite
go test -race ./...    # what CI runs
go vet ./...
gofmt -l .             # must print nothing
```

CI runs the suite on both `ubuntu-latest` and `windows-latest`, so avoid
path- and line-ending-sensitive assumptions in tests.

## Changing compression behaviour

Any change that alters output bytes for existing inputs needs:

- a test that pins the new behaviour on a fixture, and
- a note in [CHANGELOG.md](CHANGELOG.md) saying what output changed and why.

If you change the savings numbers, re-measure and update the README rather than
leaving the old figures in place — quoted numbers should be reproducible from
`testdata/`.

## Public API changes

`squoze.go` is the embedding contract (`NewEngine`, `Process`, `Engine`,
`Result`, `Version`, `DefaultMemoCapacity`). Additions are fine; renames and
signature changes break embedders and need a version bump plus a CHANGELOG
entry calling out the break.

## Commits and pull requests

- One logical change per commit; imperative subject line (`fix: …`, `feat: …`,
  `docs: …`).
- Tests green and `gofmt` clean before you commit.
- In the PR, say what changed, how you verified it, and whether output bytes
  changed for any existing input.

## Security

Do not open a public issue for vulnerabilities — see
[SECURITY.md](SECURITY.md).
