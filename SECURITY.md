# Security Policy

## Supported versions

Fixes land on `main` and ship in the next tagged release. Older tags are not
backported — upgrade to the newest `v0.1.x`.

## Reporting a vulnerability

**Do not open a public issue for security reports.**

Use GitHub private vulnerability reporting: *Security → Report a vulnerability*
on this repository. Please include:

- affected version or commit (`squoze --version` prints it);
- a minimal reproduction — the input bytes and mode are usually enough;
- impact assessment.

You will get an acknowledgement within 72 hours. Reporters are credited in the
release notes unless you prefer otherwise.

## Threat model

squoze is a pure byte-to-byte transform plus a small CLI. What that means for
severity:

- **The engine makes no network calls and reads no files.** `internal/engine`
  operates only on the buffer handed to it. Bugs there are correctness or
  denial-of-service issues (pathological input causing excessive CPU or memory),
  not data exfiltration.
- **Zero third-party dependencies**, so there is no transitive supply-chain
  surface — the standard library is the whole dependency tree.
- **`livecheck` and `oc` are the only subcommands that touch the outside
  world.** They talk to endpoints you configure and may read local agent config
  files when wiring (`--auto`). Credential handling there is in scope.
- **Embedders** (for example the gateway in
  [2papi](https://github.com/Rethinger/2papi)) pass request bodies through the
  engine. A bug that leaks one caller's bytes into another caller's output — for
  instance through the memoization cache — is high severity; report it.

### In scope

- Content from one `Apply` / `Process` call surfacing in another's output.
- `mustKeep` regions being altered or dropped (a guarantee violation).
- Non-deterministic output for identical input.
- Input that triggers unbounded CPU or memory growth.
- Credential mishandling in `livecheck` / `oc`, including writing secrets to
  logs or config backups.

### Not in scope

- **Windows Defender flagging the installed binary.** Freshly built, unsigned Go
  binaries are a known false positive for SmartScreen/Defender heuristics. It is
  a distribution annoyance, not a vulnerability — see
  [docs/UNFINISHED.md](docs/UNFINISHED.md).
- Lossy compression *outside* `mustKeep` regions. Dropping content there is the
  documented purpose of the tool, not a defect.
- Findings that require an attacker to already control the process running
  squoze.
