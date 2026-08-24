# squoze

> Your context, squoze.

**Universal, deterministic LLM context optimizer.** One static binary that sits
between your AI agent and any OpenAI/Anthropic-compatible provider and shrinks
request bodies — without losing information the model needs, and without
breaking provider prompt caches.

**Status: experimental, MVP step 1 of 8.** Right now squoze detects the wire
format and passes everything through untouched while reporting byte counts.
Compression transforms land in steps 2–5 (see `docs/plan` in the source repo).

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
```

Every response carries `X-Squoze-Original-Bytes`, `X-Squoze-Sent-Bytes` and
`X-Squoze-Format`.

## Build

```sh
go build ./...
go test ./...
```

Apache-2.0.
