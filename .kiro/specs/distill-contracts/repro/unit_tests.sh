#!/bin/sh
# Юниты + vet всего модуля. Аргументы уходят в go test как есть.
#
#   MSYS_NO_PATHCONV=1 docker run --rm \
#     -v "$PWD:/w" -w /w golang:1.23 \
#     sh /w/.kiro/specs/distill-contracts/repro/unit_tests.sh ./...
#
# MSYS_NO_PATHCONV=1 обязателен из Git Bash: иначе "C:\...:/w" превращается
# в "'W:/' is invalid".
export PATH=$PATH:/usr/local/go/bin
export GOTOOLCHAIN=local
export GOFLAGS=-mod=mod
cd /w || exit 1
go build ./... 2>&1 | tail -20 || exit 1
go vet ./... 2>&1 | tail -20
go test "$@" 2>&1 | tail -45
