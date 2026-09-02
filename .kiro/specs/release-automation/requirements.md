# Requirements — squoze release automation

Status: in progress · Created 2026-09-02 · Quick Spec (design inline)

## Context / problem

squoze has three tags but only two releases, and nothing is automated:

| Tag | Date | GitHub Release |
|---|---|---|
| `v0.1.0` | 2026-08-24 | published 2026-08-28 (by hand) |
| `v0.1.1` | 2026-08-24 | published 2026-08-28 (by hand) |
| `v0.1.2` | 2026-09-02 | **none** |

`.github/workflows/ci.yml` triggers only on `push: branches: [main]` and
`pull_request` — pushing a tag runs nothing. There is no `.goreleaser.yaml`. So
the only way a release has ever appeared is a human clicking through the GitHub
UI, and v0.1.2 — the release that first made squoze embeddable as a library —
was missed.

Consequence: the README documents
`go install github.com/Rethinger/squoze/cmd/squoze@latest`, which works because
the Go module proxy serves tags directly, but there are **no downloadable
binaries** for anyone without a Go toolchain.

Sibling precedent: 2papi has a working `.goreleaser.yaml` (verified to build 5
targets) — see [../../../2papi/.kiro/specs/release-automation/requirements.md].

## Functional requirements

- **FR-1 (Must)** — WHEN a tag matching `v*` is pushed, the system SHALL build
  release artifacts and publish them as a GitHub Release for that tag.
  - **AC-1.1** — Given tag `vX.Y.Z` is pushed, When CI completes, Then a
    published (non-draft) GitHub Release `vX.Y.Z` exists carrying archives and a
    checksums file.
  - **AC-1.2** — Given the release job runs, When it publishes, Then it uses only
    the workflow's `GITHUB_TOKEN` — no long-lived personal token.

- **FR-2 (Must)** — The system SHALL build the `squoze` CLI for linux, darwin and
  windows on amd64 and arm64 (excluding windows/arm64), as static
  `CGO_ENABLED=0` binaries.
  - **AC-2.1** — Given a release, Then archives exist for each supported
    platform pair, `.tar.gz` for linux/darwin and `.zip` for windows.

- **FR-3 (Must)** — The release SHALL NOT publish if tests fail.
  - **AC-3.1** — Given a tag push where `go test -race ./...` fails on any matrix
    OS, When the workflow runs, Then no release is created.

- **FR-4 (Must)** — The binary SHALL report the released version.
  - **AC-4.1** — Given a released binary of `vX.Y.Z`, When `squoze version` runs,
    Then it prints `squoze vX.Y.Z`.
  - Note: `Version` is a `const` in `internal/engine`, not an ldflags variable,
    so this is a source-hygiene requirement — the constant must be bumped before
    tagging. See [../cli-ux/bugfix.md](../cli-ux/bugfix.md), which fixed it
    reading `0.1.0` at tag `v0.1.2`.

- **FR-5 (Should)** — The missing `v0.1.2` release SHALL be published
  retroactively so the tag history and release history agree.

- **FR-6 (Should)** — Release notes SHALL be generated from commit history, with
  `docs:` and `test:` commits filtered out.

## Non-functional requirements

- **NFR-1** — Zero dependencies stays true: goreleaser is CI tooling, never a
  module dependency. `go.mod` must remain free of `require` entries.
- **NFR-2** — `goreleaser check` must pass with no deprecation warnings, so a
  future goreleaser major does not silently break releases.
- **NFR-3** — Workflow permissions least-privilege: `contents: write` only on the
  release job; the existing test/fmt jobs keep `contents: read`.
- **NFR-4** — Archives ship `README.md` and `LICENSE` so a downloaded binary
  carries its license.

## Constraints

- Unsigned binaries trigger Windows Defender/SmartScreen false positives — a
  known, documented distribution annoyance (see `docs/UNFINISHED.md`), not
  solved here. Code signing is out of scope.
- Homebrew/scoop taps are out of scope (no companion repos exist).

## Edge cases

- Re-pushing an existing tag: goreleaser fails on a release that already exists
  rather than overwriting. Acceptable — treated as operator error.
- Pre-release tags (`v0.2.0-rc1`): `prerelease: auto` marks them pre-release
  from the tag's semver, so they do not become "Latest".
