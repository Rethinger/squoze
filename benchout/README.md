# Сырые выходы бенчмарков

Не редактируются руками — только перезаписываются скриптами из
[`../.kiro/specs/distill-contracts/repro/`](../.kiro/specs/distill-contracts/repro/).
Лежат в репозитории как доказательство под цифрами в `tasks.md`: там, где
указано число, должна быть команда, которая его воспроизводит.

| Файл | Чем снят | Что показывает |
|---|---|---|
| `old.txt` | `repro/bench_ab.sh` | все бенчмарки на теге v0.2.0, N=12 |
| `new.txt` | `repro/bench_ab.sh` | то же на рабочем дереве |
| `benchstat.txt` | `repro/bench_ab.sh` | сведённая таблица old → new, geomean sec/op −1.30% (NFR-1). Скрипт сначала срезает из обоих файлов строки `go: downloading` в `*.clean.txt` — benchstat принимает их за метаданные конфигурации и отказывается сводить дельту; эти два файла производные и в репозитории не лежат |
| `router_classify.before.txt` | `repro/router_bench.sh` до правки FR-8 | базовая таблица TSK-014; единственная аллокация роутера — 24 Б на `json_over_probe` |
| `router_classify.after.txt` | `repro/router_bench.sh` после правки | та же таблица: 24 Б → 0, 1 alloc → 0 (p=0.002) |
| `router_json_probe.txt` | `repro/router_bench.sh` | цена, снятая гардом, замер внутри одного процесса (TSK-015) |

`router_classify.before.txt` и `.after.txt` — единственная пара, которую скрипт
не пишет сам: это снимки до и после правки, снятые вручную тем же
`router_bench.sh` с переименованием. Повторный прогон перезапишет
`router_classify.txt`, не их.
