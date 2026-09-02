# Tasks — squoze release automation

Requirements: [requirements.md](requirements.md)

## Phase 1 — Build config

- [x] **TSK-001**: Add `.goreleaser.yaml`.
  - Requirement: FR-2, FR-6, NFR-2, NFR-4
  - Deliverables: `.goreleaser.yaml`
  - Detail: `version: 2`; one build from `./cmd/squoze` producing binary
    `squoze`, `CGO_ENABLED=0`, `-s -w` ldflags; goos linux/darwin/windows ×
    goarch amd64/arm64 minus windows-arm64; archives named
    `squoze_{{.Os}}_{{.Arch}}` with zip override for windows; ship `README.md`
    and `LICENSE`; `checksum` + changelog filters excluding `^docs:` and
    `^test:`; `release: prerelease: auto`. Use the modern spellings (`ids:`,
    `formats:`) — not the deprecated `builds:` / `format_overrides.format` that
    2papi's config still uses.
  - Acceptance: `goreleaser check` passes with **no** deprecation warnings.

- [x] **TSK-002**: Verify the config builds without publishing.
  - Requirement: FR-2, AC-2.1
  - Deliverables: none (verification step)
  - Detail: `goreleaser release --snapshot --clean --skip=publish` in a
    container; confirm five archives and `checksums.txt` in `dist/`.
  - Acceptance: snapshot succeeds; `dist/` holds the expected archive set.

- [x] **TSK-003**: Ensure `dist/` is ignored.
  - Requirement: hygiene
  - Deliverables: `.gitignore`
  - Acceptance: after a snapshot build, `git status --short` is clean.

## Phase 2 — CI wiring

- [x] **TSK-004**: Trigger CI on `v*` tags.
  - Requirement: FR-1
  - Deliverables: `.github/workflows/ci.yml`
  - Detail: add `tags: ['v*']` to `on.push`. Without this the release job is
    unreachable no matter how it is gated — the exact defect found in 2papi.
  - Acceptance: pushing a tag starts a workflow run.

- [x] **TSK-005**: Add the gated release job.
  - Requirement: FR-1, FR-3, AC-1.2, NFR-3
  - Deliverables: `.github/workflows/ci.yml`
  - Detail: job `release`, `needs: [test, fmt]`,
    `if: startsWith(github.ref, 'refs/tags/v')`, `permissions: contents: write`,
    `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs full history
    for notes), `setup-go` with `go-version-file: go.mod`, then
    `goreleaser-action@v6` with `args: release --clean` and
    `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`.
  - Acceptance: on a branch push the job is *skipped*; on a `v*` tag it runs
    after tests pass and publishes.

## Phase 3 — Backfill and verify

- [ ] **TSK-006**: Publish the missing `v0.1.2` release.
  - Requirement: FR-5
  - Deliverables: a published GitHub Release for `v0.1.2`
  - Detail: the automation only fires on *new* tag pushes, so `v0.1.2` (already
    pushed) will not retrigger. Cut the next tag once the CLI fix from
    [../cli-ux/tasks.md](../cli-ux/tasks.md) lands, and let automation produce
    it; retro-publishing v0.1.2 by hand is the fallback if the history must
    match exactly.
  - Acceptance: no tag remains without a corresponding release.

- [ ] **TSK-007**: Verify end to end on a real tag.
  - Requirement: AC-1.1, AC-2.1, AC-4.1
  - Detail: after the tag push, confirm the run's release job succeeded, the
    Release lists archives + `checksums.txt`, and a downloaded linux/amd64
    binary prints the matching `squoze vX.Y.Z`.
  - Acceptance: all three hold.

## Dependency graph

```
TSK-001 → TSK-002 → TSK-003
                      ↓
TSK-004 → TSK-005 ──→ TSK-006 → TSK-007
```

Config is proven locally before CI is wired; the tag push (TSK-006) is the first
live exercise, verified by TSK-007.

## Progress

| Task | Status |
|---|---|
| TSK-001 .goreleaser.yaml | Complete |
| TSK-002 snapshot verification | Complete |
| TSK-003 dist/ ignored | Complete |
| TSK-004 tags trigger | Complete |
| TSK-005 release job | Complete |
| TSK-006 backfill v0.1.2 | Pending |
| TSK-007 end-to-end verify | Pending |
