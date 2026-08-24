package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOriginalsPutGetRoundtrip(t *testing.T) {
	o := NewOriginals()
	text := []byte("verbose original output\nwith many lines\n")
	ref, err := o.Put(text)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := o.Get(ref)
	if !ok || got != string(text) {
		t.Fatalf("roundtrip failed: ok=%v", ok)
	}
	if RefOf(text) != ref {
		t.Fatal("RefOf inconsistent with Put")
	}
}

func TestOriginalsPersistAndReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	o1, err := OpenOriginals(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := []byte("first original")
	b := []byte("second original with more content\nline two\n")
	if _, err := o1.Put(a); err != nil {
		t.Fatal(err)
	}
	if _, err := o1.Put(b); err != nil {
		t.Fatal(err)
	}
	// Re-put of identical content must not duplicate.
	if _, err := o1.Put(a); err != nil {
		t.Fatal(err)
	}

	o2, err := OpenOriginals(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{string(a), string(b)} {
		got, ok := o2.Get(RefOf([]byte(want)))
		if !ok || got != want {
			t.Fatalf("reload lost content %q", want[:12])
		}
	}
}

func TestResolveByPrefix(t *testing.T) {
	dir := t.TempDir()
	o, _ := OpenOriginals(dir)
	long := []byte("the full unrecoverable-without-this tool output")
	ref, _ := o.Put(long)

	got, err := o.Resolve(ref[:8])
	if err != nil || got != string(long) {
		t.Fatalf("prefix resolve failed: %v", err)
	}
	if _, err := o.Resolve("ffffffffffff"); err == nil {
		t.Fatal("missing ref must error")
	}
}

func TestMemoryOnlyStoreWritesNothing(t *testing.T) {
	dir := t.TempDir()
	o := NewOriginals() // no dir
	o.Put([]byte("ephemeral"))
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("memory-only store wrote files: %v %d", err, len(entries))
	}
}
