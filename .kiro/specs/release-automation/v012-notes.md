Agent wiring and one-command lifecycle.

This tag was pushed before release automation existed, so it never got a Release
page. Publishing it retroactively for a complete history — **no binaries here**:
prebuilt archives start with [v0.1.3](https://github.com/Rethinger/squoze/releases/tag/v0.1.3),
which contains everything below plus CLI fixes.

## Agents

- `squoze <agent>` one-command lifecycle — dynamic per-request upstream via the
  routing header, auto-launch, auto-unwind.
- Automatic config wiring (`--auto` / `--unwire`) for `opencode` and `omp`, with
  backups. `--auto` is the default for config agents, so `squoze oc` just works;
  `--manual` opts out.
- Harness presets for claude / openai / gemini / deepseek / openrouter / fireworks.
- Agent-level wiring: opencode/omp snippets plus env-agent wrap presets.
- `CMD` override for config agents (headless testing).

## Providers

- All providers wired, from config and catalog defaults; OAuth rides the routing header.
- Per-provider listener ports, working around opencode#5674 for custom
  OpenAI-compatible providers.
- Provider targeting (`--provider` plus auto-detect), baseURL path preservation,
  and BOM-tolerant parsing.

## Other

- Library facade package for gateway embedding.
- `--log FILE` — full JSONL request log for `proxy` and `wrap`.
- Fixed proxy double-join on the provider prefix and the headless `CMD` override.
