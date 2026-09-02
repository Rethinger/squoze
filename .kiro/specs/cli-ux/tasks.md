# Tasks — squoze CLI usability

Bugfix spec: [bugfix.md](bugfix.md)

## Phase 1 — CLI entry point

- [x] **TSK-001**: Accept version flags as aliases of the `version` subcommand.
  - Requirement: bugfix.md → Expected behavior, row 1
  - Deliverables: `cmd/squoze/main.go`
  - Detail: handle `version`, `-v`, `-version`, `--version` in one `case`,
    printing `squoze v<engine.Version>` to stdout, exit 0. Match 2papi's
    `cmd/gateway/main.go:55` convention.
  - Acceptance: all four spellings print the identical line and exit 0.

- [x] **TSK-002**: Add explicit help handling.
  - Requirement: bugfix.md → Expected behavior, row 2
  - Deliverables: `cmd/squoze/main.go`
  - Detail: `help`, `-h`, `--help` write full usage to **stdout** and exit 0.
    Split `usage()` into a writer-taking helper so no-args/unknown paths keep
    writing to stderr with exit 2.
  - Acceptance: `squoze --help` exits 0 and its output is redirectable with `>`.

- [x] **TSK-003**: Name the unknown command in the error path.
  - Requirement: bugfix.md → Expected behavior, row 4
  - Deliverables: `cmd/squoze/main.go`
  - Detail: `default:` prints `squoze: unknown command "<arg>"` to stderr before
    usage; exit stays 2.
  - Acceptance: `squoze bogus` names `bogus` and still exits 2.

- [x] **TSK-004**: Make usage list the real dispatch table.
  - Requirement: bugfix.md → Current behavior (usage drift)
  - Deliverables: `cmd/squoze/main.go`
  - Detail: list `proxy`, `wrap`, `agent`, `harness`, `livecheck`, `retrieve`,
    `version`, `help`; add a line noting agent names double as top-level
    aliases (`squoze oc` ≡ `squoze agent opencode`). Refresh the stale package
    doc comment at the top of the file.
  - Acceptance: every `case` in `main()` appears in usage output.

## Phase 2 — Regression guard

- [x] **TSK-005**: Table test over the CLI surface.
  - Requirement: bugfix.md → Regressions 1-3
  - Deliverables: `cmd/squoze/main_test.go`
  - Detail: build the binary via `go test` and exercise the invocation table
    (invocation → exit code → first line / stream). Pin `squoze version`
    output and bare-`squoze` exit 2 so the fix cannot regress. Keep it
    hermetic — no network, no listening sockets.
  - Acceptance: test fails if any row of the Expected-behavior table breaks.

## Phase 3 — Docs truth-up

- [x] **TSK-006**: Correct docs that describe the old surface.
  - Requirement: bugfix.md → Regressions 5
  - Deliverables: `SECURITY.md`, `README.md`
  - Detail: `SECURITY.md` already tells reporters to run `squoze --version` —
    true once TSK-001 lands; verify rather than reword. Ensure README's command
    list matches the new usage text.
  - Acceptance: every command spelling quoted in docs works against the built
    binary.

## Dependency graph

```
TSK-001 ─┐
TSK-002 ─┼─→ TSK-005 ─→ TSK-006
TSK-003 ─┤
TSK-004 ─┘
```

TSK-001..004 all edit `main.go` and land together; TSK-005 pins them; TSK-006
verifies docs last.

## Progress

| Task | Status |
|---|---|
| TSK-001 version flag aliases | Complete |
| TSK-002 help handling | Complete |
| TSK-003 unknown-command message | Complete |
| TSK-004 usage lists all subcommands | Complete |
| TSK-005 CLI table test | Complete |
| TSK-006 docs truth-up | Complete |

## Evidence

Built and exercised in a `golang:1.22` container against the working tree:

| Invocation | Exit | Stream | Output |
|---|---|---|---|
| `version` / `--version` / `-v` / `-version` | 0 | stdout | `squoze v0.1.2` |
| `help` / `--help` / `-h` | 0 | stdout | full usage |
| `bogus-cmd` | 2 | stderr | `squoze: unknown command "bogus-cmd"` + usage |
| *(no args)* | 2 | stderr | usage |

`go vet ./...` clean; `go test -count=1 -race ./...` green across all 11
packages (`cmd/squoze` 46.7s, includes the new table test). `gofmt` clean on
LF-normalised sources — the working tree's CRLF checkout makes `gofmt -l` list
files locally, but CI checks out LF and its gate passes.

Regressions from the spec, all verified: the agent alias still resolves
identically via `squoze oc` and `squoze agent opencode` (asserted in
`TestAgentAliasStillResolves`); `squoze version` output is byte-pinned against
`engine.Version`; bare `squoze` still exits 2; every docs spelling works.
