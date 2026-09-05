#!/bin/sh
# A/B бенчмарки: тег v0.2.0 против рабочего дерева, benchstat с p-values.
# Нужен golang:1.26 — benchstat требует Go >= 1.26.
#
#   MSYS_NO_PATHCONV=1 docker run --rm \
#     -v "C:/Users/rethi/Documents/Projects/squoze:/w" -w /w golang:1.26 \
#     sh /w/.kiro/specs/distill-contracts/repro/bench_ab.sh
#
# N=<reps> переопределяет число прогонов (по умолчанию 12).
# Уже существующие benchout/old.txt и new.txt не перезаписываются: снять их
# заново — удалить файл. Сравнение идёт по *.clean.txt, потому что benchstat
# принимает строки "go: downloading ..." за метаданные конфигурации и
# отказывается сводить два файла в одну таблицу дельт.
export PATH=$PATH:/usr/local/go/bin:/root/go/bin
export GOTOOLCHAIN=local
export GOFLAGS=-mod=mod
set -e
PKGS="./internal/engine/ ./internal/proxy/ ./internal/router/"
N=${N:-12}
OUT=/w/benchout
mkdir -p "$OUT"
go version

if [ ! -f "$OUT/old.txt" ]; then
  echo "=== baseline v0.2.0 ==="
  rm -rf /tmp/base
  git clone -q --branch v0.2.0 /w /tmp/base
  (cd /tmp/base && go test -run '^$' -bench . -benchmem -count="$N" $PKGS) > "$OUT/old.txt" 2>&1
  tail -2 "$OUT/old.txt"
fi
if [ ! -f "$OUT/new.txt" ]; then
  echo "=== HEAD (working tree) ==="
  (cd /w && go test -run '^$' -bench . -benchmem -count="$N" $PKGS) > "$OUT/new.txt" 2>&1
  tail -2 "$OUT/new.txt"
fi

sed -n '/^goos:/,$p' "$OUT/old.txt" > "$OUT/old.clean.txt"
sed -n '/^goos:/,$p' "$OUT/new.txt" > "$OUT/new.clean.txt"
go install golang.org/x/perf/cmd/benchstat@latest 2>&1 | tail -3
echo "=== benchstat old -> new ==="
benchstat "$OUT/old.clean.txt" "$OUT/new.clean.txt" | tee "$OUT/benchstat.txt"
