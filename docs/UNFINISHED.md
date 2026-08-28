# squoze — недоделанное / что дальше

Дата: 2026-08-28. Универсальный оптимизатор контекста для LLM-шлюзов и
агентов (head/tail элизия, reversible, zero-dependency).

## В работе / известные долги

- **Defender false positive**: свежесобранный `sq.exe` карантинится Windows
  Defender (ThreatID 251873, «virus or potentially unwanted software») —
  воспроизводится на чистой сборке, старые бинари не трогались. Нужно:
  подпись (signtool/self-signed) ИЛИ документация с `Add-MpPreference`
  exclusion. Временный фикс уже применён на машине владельца.
- **Windows-сборка только через docker**: на машине нет Go-тулчейна.
  `GOOS=windows` сборка идёт контейнером `golang:1.22`.
- **L2-eval не прогнан**: LoCoMo/RULER/BFCL (Δaccuracy ≤2pp, savings ≥50%,
  overhead <15ms p95) — нужны API-ключи провайдеров (release-gate, протокол
  в `docs/eval-protocol.md`). L1 (fixtures каждый коммит) — зелёный.
- **goreleaser не опубликован**: модуль `github.com/Rethinger/squoze` живёт
  локально + `replace` в 2papi; публикация тега/v1.0.0 откроет
  `go get github.com/Rethinger/squoze` без replace.
- **livecheck против большего числа провайдеров**: проверено на Fireworks
  (95.8% на 51KB, reasoning_content fallback работает); Anthropic/OpenAI/
  DeepSeek форварды — юнит-тесты, живые не гонялись.
- **`sq oc` headless**: `--log sq.jsonl` пуст при запуске opencode run в
  не-TTY — диагностировано: opencode не шлёт запросы в non-interactive
  окружении (не баг squoze), проверять в TTY владельца.

## План squoze v2 (после 2papi G1-G4)

- **Question-aware elision**: выбирать сохраняемые строки по релевантности
  к вопросу (эвристика, не нейросеть — сохранить 0.43ms/zero-dep).
- **JSON structural pruning**: сжимать безопасные JSON-блоки структурно
  (запланировано вторым проходом router).
- **Кэш-экономика**: governance по примеру B3 (2papi): не сжимать, когда
  промпт-кэш провайдера выгоднее байтов.

## Доказано (не потерять)

- 94.8% / 92.6% / 93.9% savings на go_test/pytest/logs фикстурах (живой
  Fireworks, 200 OK, mustKeep дословно — 420/4200, refund, ERROR).
- memo LRU: повтор → memo_hits 1, байт-идентично.
- 17 провайдеров владельца вшиваются одной командой `sq oc` (auto по
  умолчанию); фейк-провайдер для e2e — `sq.exe oc -- opencode run`.
- Фикс double-join пути (commit `20e4341`) — без него `/inference/v1` дублировался → 404.