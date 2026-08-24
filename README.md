# squoze

> Your context, squoze.

**Universal, deterministic LLM context optimizer.** One static binary that sits
between your AI agent and any OpenAI/Anthropic-compatible provider and shrinks
request bodies — without losing information the model needs, and without
breaking provider prompt caches.

**Status: experimental, v0.1.0 (MVP steps 1–5 shipped).** squoze detects the
wire format, classifies content blocks and squeezes verbose machine output
(test runs, logs) with head/tail elision that never drops error lines.
Measured on a 270KB go-test blob: **98.9% byte savings** while keeping every
`--- FAIL` line and a stable marker for provider prompt caches. Compression
presets adapt per model family, and every elision is locally reversible by
ref.

## Why

Agents waste most of their token budget on noise: repeated file reads, verbose
CLI output, stale tool results. squoze removes the noise and keeps the signal —
deterministically, with quality contracts:

- **Fail-open** — any error means your request goes through untouched.
- **Never-elide** — errors, failures and stack traces are never compressed away.
- **Cache-safe** — decisions are stable across turns so provider prompt caches
  keep hitting (a 90% discount is worth more than a 20% squeeze).
- **Reversible** — originals stay local; the model can pull them back on demand.

## Usage

```sh
# Drop-in proxy for any OpenAI/Anthropic-compatible client
squoze proxy --port 8787 --upstream https://api.anthropic.com
ANTHROPIC_BASE_URL=http://localhost:8787 claude

# Or zero-config: wrap the agent, env injection does the rest
squoze wrap --upstream https://api.anthropic.com claude

# Resolve an elision marker ref back to the full original text
squoze retrieve a3f9c2e1b4d0
```

Every response carries `X-Squoze-Original-Bytes`, `X-Squoze-Sent-Bytes` and
`X-Squoze-Format`.

## Build

```sh
go build ./...
go test ./...            # unit + quality contracts
go test ./internal/eval -v   # was→is savings table
```

Apache-2.0. See `docs/eval-protocol.md` for the full evaluation strategy.
