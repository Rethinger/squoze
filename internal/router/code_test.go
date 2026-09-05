package router

import (
	"strconv"
	"strings"
	"testing"
)

// goSourceEmbeddingTestOutput is the shape that motivated KindCode: a fixture
// generator whose whole job is to hold machine output verbatim. Before the
// detector existed this classified as KindTestOutput and got elided, which
// silently rewrote a source file the caller was about to compile.
const goSourceEmbeddingTestOutput = `package squozebench

import "strings"

// Corpus returns synthetic go test output for the savings benchmark.
func Corpus(n int) string {
	var b strings.Builder
	b.WriteString("=== RUN   TestAlpha\n")
	b.WriteString("--- FAIL: TestAlpha (0.01s)\n")
	b.WriteString("    alpha_test.go:31: want 4 got 5\n")
	for i := 0; i < n; i++ {
		b.WriteString("=== RUN   TestBeta\n")
		b.WriteString("--- PASS: TestBeta (0.00s)\n")
	}
	b.WriteString("FAIL\n")
	b.WriteString("exit status 1\n")
	return b.String()
}

type Case struct {
	Name string
	Want int
}

func Cases() []Case {
	return []Case{
		{Name: "assert AssertionError", Want: 1},
		{Name: "pytest PASSED", Want: 2},
	}
}
`

func TestCodeIsNotClassifiedAsTestOutput(t *testing.T) {
	if got := Classify(goSourceEmbeddingTestOutput); got != KindCode {
		t.Fatalf("fixture generator classified as %v, want code — it would be elided", got)
	}
}

func TestCodeDetectorAcrossLanguages(t *testing.T) {
	py := `#!/usr/bin/env python3
import os
import sys

from collections import defaultdict


class Runner:
    def __init__(self, root):
        self.root = root

    def walk(self):
        counts = defaultdict(int)
        for base, _, files in os.walk(self.root):
            for f in files:
                counts[f] += 1
        return counts


def main():
    r = Runner(sys.argv[1])
    print(r.walk())
`
	ts := `import { readFile } from "node:fs/promises";

export interface Options {
  root: string;
  strict: boolean;
}

export async function load(opts: Options): Promise<string[]> {
  const raw = await readFile(opts.root, "utf8");
  return raw.split("\n").filter((l) => l.length > 0);
}

const DEFAULTS: Options = { root: ".", strict: false };

export default DEFAULTS;
`
	rs := `use std::collections::HashMap;

pub struct Counter {
    counts: HashMap<String, usize>,
}

impl Counter {
    pub fn new() -> Self {
        Counter { counts: HashMap::new() }
    }

    pub fn add(&mut self, key: &str) {
        *self.counts.entry(key.to_string()).or_insert(0) += 1;
    }
}
`
	licensed := `// Copyright 2026 The Squoze Authors.
//
// Licensed under the Apache License, Version 2.0.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package widget

import "fmt"

type Widget struct {
	Name string
}

func (w Widget) String() string {
	return fmt.Sprintf("widget %s", w.Name)
}
`
	cases := []struct {
		name string
		in   string
	}{
		{"python", py},
		{"typescript", ts},
		{"rust", rs},
		{"go behind a license header", licensed},
	}
	for _, tc := range cases {
		if got := Classify(tc.in); got != KindCode {
			t.Errorf("%s: got %v want code", tc.name, got)
		}
	}
}

// TestRealTestOutputStillWins is the anti-regression for the detector: these
// blobs are what the compressor exists for. If the code detector steals any of
// them, savings collapse.
func TestRealTestOutputStillWins(t *testing.T) {
	panicking := `=== RUN   TestWorker
--- FAIL: TestWorker (0.00s)
panic: runtime error: index out of range [5] with length 3 [recovered]

goroutine 21 [running]:
testing.tRunner.func1.2({0x104d0a0, 0xc0000180a8})
	/usr/local/go/src/testing/testing.go:1631 +0x24a
testing.tRunner.func1()
	/usr/local/go/src/testing/testing.go:1634 +0x377
panic({0x104d0a0?, 0xc0000180a8?})
	/usr/local/go/src/runtime/panic.go:770 +0x132
github.com/acme/worker.process(...)
	/src/worker/process.go:88
github.com/acme/worker.TestWorker(0xc000106340)
	/src/worker/worker_test.go:42 +0x18f
testing.tRunner(0xc000106340, 0x10a1e80)
	/usr/local/go/src/testing/testing.go:1689 +0xfb
created by testing.(*T).Run in goroutine 1
	/usr/local/go/src/testing/testing.go:1742 +0x390
FAIL	github.com/acme/worker	0.213s
FAIL
exit status 1
`
	// pytest echoes the failing source under the header. That echoed `def ` is
	// exactly why openers must sit at column 0.
	pytestEcho := `============================= test session starts =============================
collected 3 items

tests/test_math.py F                                                    [ 33%]

================================== FAILURES ===================================
_________________________________ test_thing __________________________________

    def test_thing():
        result = add(1, 2)
>       assert result == 4
E       assert 3 == 4

tests/test_math.py:7: AssertionError
=========================== short test summary info ===========================
FAILED tests/test_math.py::test_thing - assert 3 == 4
1 failed, 2 passed in 0.04s
`
	// Sized like a real broken build, not a 4-line snippet: blobs under
	// profile MinBytes pass through untouched whatever their kind, so a tiny
	// fixture would assert nothing about routing.
	var ce strings.Builder
	ce.WriteString("# github.com/acme/worker\n")
	for i := 0; i < 60; i++ {
		ce.WriteString("./process.go:")
		ce.WriteString(strconv.Itoa(12 + i))
		ce.WriteString(":6: undefined: helperFunctionWithARatherLongName\n")
	}
	ce.WriteString("FAIL\tgithub.com/acme/worker [build failed]\n")
	compileErr := ce.String()
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"go test with panic trace", panicking, KindTestOutput},
		{"pytest echoing source", pytestEcho, KindTestOutput},
		{"go build failure", compileErr, KindTestOutput},
	}
	for _, tc := range cases {
		if got := Classify(tc.in); got != tc.want {
			t.Errorf("%s: got %v want %v — compression would be skipped", tc.name, got, tc.want)
		}
	}
}

func TestCodeDetectorNeedsDensity(t *testing.T) {
	// A single declaration inside a paragraph is prose, not a file.
	in := "func is a keyword in Go and Rust alike. " +
		strings.Repeat("The migration plan needs review before Friday. ", 40)
	if got := Classify(in); got == KindCode {
		t.Errorf("prose mentioning a keyword classified as code")
	}
}

func TestClassifyCodeDeterministic(t *testing.T) {
	first := Classify(goSourceEmbeddingTestOutput)
	for i := 0; i < 8; i++ {
		if got := Classify(goSourceEmbeddingTestOutput); got != first {
			t.Fatalf("non-deterministic: %v vs %v", got, first)
		}
	}
}
