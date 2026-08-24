// Package store provides the content-addressed decision memo behind the
// cache-guard contract.
//
// Why it exists: provider prompt caches give ~90% discounts for stable
// prefixes. The engine's transforms are deterministic today, but determinism
// is a property of CURRENT parameters — config reloads, preset changes or
// algorithm upgrades would silently rewrite previously-sent blocks and break
// caches mid-session. The memo pins decisions by content hash: the same
// original bytes always yield the exact same compressed bytes for the
// lifetime of the process, regardless of what parameters say now.
package store

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

// Memo is a bounded LRU map from SHA256(original) to compressed bytes.
// Safe for concurrent use.
type Memo struct {
	mu    sync.Mutex
	cap   int
	m     map[[32]byte]*list.Element
	order *list.List // front = most recent
}

type entry struct {
	key  [32]byte
	orig int // original size, kept for hit-rate accounting
	val  []byte
}

// NewMemo returns an LRU holding up to cap entries (cap <= 0 = disabled).
func NewMemo(capacity int) *Memo {
	if capacity <= 0 {
		return nil
	}
	return &Memo{
		cap:   capacity,
		m:     make(map[[32]byte]*list.Element, capacity),
		order: list.New(),
	}
}

func key(b []byte) [32]byte {
	return sha256.Sum256(b)
}

// Get returns the pinned output for orig, if seen before.
func (m *Memo) Get(orig []byte) ([]byte, bool) {
	if m == nil {
		return nil, false
	}
	k := key(orig)
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.m[k]
	if !ok {
		return nil, false
	}
	m.order.MoveToFront(el)
	out := el.Value.(*entry).val
	// Return a copy so callers can never mutate pinned decisions.
	cp := make([]byte, len(out))
	copy(cp, out)
	return cp, true
}

// Put pins a decision. Overwrites on re-put of the same key (should not
// happen when used correctly, but must never corrupt state).
func (m *Memo) Put(orig, comp []byte) {
	if m == nil {
		return
	}
	k := key(orig)
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.m[k]; ok {
		el.Value.(*entry).val = append([]byte(nil), comp...)
		m.order.MoveToFront(el)
		return
	}
	el := m.order.PushFront(&entry{key: k, orig: len(orig), val: append([]byte(nil), comp...)})
	m.m[k] = el
	for m.order.Len() > m.cap {
		old := m.order.Back()
		if old == nil {
			break
		}
		m.order.Remove(old)
		delete(m.m, old.Value.(*entry).key)
	}
}

// Len reports current occupancy.
func (m *Memo) Len() int {
	if m == nil {
		return 0
	}
	return m.order.Len()
}
