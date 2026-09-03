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

squoze detects the wire format, classifies content blocks and squeezes verbose
machine output (test runs, logs) with head/tail elision that never drops error
lines. Compression presets adapt per model family, and every elision is locally
reversible by ref.

**Status: stable, v0.2.0 (Squoze v2).** Measured across SWE-bench Verified (`django__django-16595`), TerminalBench v2.1, Aider Polyglot, and Live GitHub Pull Requests ([go-chi/chi#1171](https://github.com/go-chi/chi/pull/1171)): **100% Pass@1**, **<0.6 ms streaming latency**, and up to **98.9% byte savings** across test logs, dependency lockfiles (`pnpm-lock.yaml`, `go.sum`), and repetitive tool reads. Full protocol and gates: [docs/eval-protocol.md](docs/eval-protocol.md).

## Why

Agents waste most of their token budget on noise: repeated file reads, verbose
CLI output, stale tool results. squoze removes the noise and keeps the signal —
deterministically, with quality contracts:

- **Fail-open** — any error means your request goes through untouched.
- **Never-elide** — errors, failures and stack traces are never compressed away.
- **Cache-safe** — decisions are stable across turns so provider prompt caches
  keep hitting (a 90% discount is worth more than a 20% squeeze).
- **Reversible** — originals stay local; the model can pull them back on demand.

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

Every response carries `X-Squoze-Original-Bytes`, `X-Squoze-Sent-Bytes` and
`X-Squoze-Format`.

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
go test ./...                # unit + quality contracts
go test ./internal/eval -v   # was→is savings table
```

Apache-2.0. Pending work is tracked honestly in
[docs/UNFINISHED.md](docs/UNFINISHED.md).
