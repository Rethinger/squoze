#!/bin/sh
# Бенчмарки роутера (TSK-014) и цена, снятая гардом FR-8 (TSK-015).
#
#   MSYS_NO_PATHCONV=1 docker run --rm \
#     -v "$PWD:/w" -w /w golang:1.23 \
#     sh /w/.kiro/specs/distill-contracts/repro/router_bench.sh
#
# Пишет benchout/router_classify.txt и benchout/router_json_probe.txt.
# Оба бенчмарка самопроверяются: Classify-кейсы падают, если фикстура попала
# не в ту ветку, а JSONProbe — если фикстура вдруг стала парситься.
#
# Как читать router_json_probe.txt: Skipped — работа, которую гард снимает
# (≈41-49 мкс на объекте, ≈46-61 мкс на массиве, 24 Б / 1 alloc).
# Kept — 24 Б / 1 alloc: отпечаток того, что на форме с парной скобкой парсер
# по-прежнему вызывается, то есть гард фильтр, а не решение. 0 alloc в Kept
# означал бы, что бенчмарк уехал не на ту ветку.
#
# Кросс-прогонное сравнение sec/op до/после на слабой машине не разрешается:
# дрейф ±22-24% печатает "регрессии" на ветках, которых правка не касается.
# Поэтому выигрыш мерится внутри одного процесса, а аллокации — benchstat'ом.
export PATH=$PATH:/usr/local/go/bin
export GOTOOLCHAIN=local
export GOFLAGS=-mod=mod
cd /w || exit 1
mkdir -p benchout
gofmt -l internal/router/
go vet ./internal/router/ || exit 1
go test ./internal/router/ || exit 1
go test -run '^$' -bench Classify -benchmem -count="${N:-6}" ./internal/router/ 2>&1 \
  | sed -n '/^goos:/,$p' > benchout/router_classify.txt
go test -run '^$' -bench JSONProbe -benchmem -count="${N:-8}" ./internal/router/ 2>&1 \
  | sed -n '/^goos:/,$p' > benchout/router_json_probe.txt
tail -3 benchout/router_classify.txt
tail -3 benchout/router_json_probe.txt
