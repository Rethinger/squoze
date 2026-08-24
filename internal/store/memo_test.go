package store

import (
	"strings"
	"sync"
	"testing"
)

func TestMemoPinAndHit(t *testing.T) {
	m := NewMemo(16)
	orig := []byte(strings.Repeat("line\n", 100))
	comp := []byte("compressed")

	m.Put(orig, comp)
	got, ok := m.Get(orig)
	if !ok || string(got) != "compressed" {
		t.Fatalf("pin failed: ok=%v got=%q", ok, got)
	}
}

func TestMemoReturnsDefensiveCopy(t *testing.T) {
	m := NewMemo(4)
	orig := []byte("original")
	m.Put(orig, []byte("pinned"))

	got, _ := m.Get(orig)
	got[0] = 'X' // caller mutates the copy
	again, _ := m.Get(orig)
	if again[0] == 'X' {
		t.Fatal("memo leaked internal state to caller mutation")
	}
}

func TestMemoEvictsLRU(t *testing.T) {
	m := NewMemo(2)
	a, b, c := []byte("a"), []byte("b"), []byte("c")
	m.Put(a, []byte("A"))
	m.Put(b, []byte("B"))
	m.Get(a) // touch a -> b becomes LRU
	m.Put(c, []byte("C"))

	if _, ok := m.Get(b); ok {
		t.Fatal("b should have been evicted")
	}
	for k, want := range map[string][]byte{"a": []byte("A"), "c": []byte("C")} {
		if _, ok := m.Get([]byte(k)); !ok {
			t.Fatalf("%s missing after eviction", k)
		}
		_ = want
	}
}

func TestNilMemoIsSafe(t *testing.T) {
	var m *Memo
	m.Put([]byte("x"), []byte("y"))
	if _, ok := m.Get([]byte("x")); ok {
		t.Fatal("nil memo must be disabled")
	}
	if m.Len() != 0 {
		t.Fatal("nil memo len must be 0")
	}
}

func TestMemoConcurrent(t *testing.T) {
	m := NewMemo(64)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			k := []byte{byte(n)}
			m.Put(k, k)
			for j := 0; j < 100; j++ {
				_, _ = m.Get(k)
			}
		}(i)
	}
	wg.Wait()
}
