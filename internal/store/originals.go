// Package store — originals: the reversibility half of the quality
// contracts. Every squeezed blob's full original text is kept locally,
// content-addressed by SHA256, so any elision can be resolved back to exact
// bytes ("reversible" contract).
//
// Persistence is an append-only JSONL file (one record per original) in a
// user-owned directory; nothing ever leaves the machine.
package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Originals keeps full source texts keyed by content hash.
type Originals struct {
	mu   sync.RWMutex
	m    map[string]string // ref (hex sha256) -> original text
	dir  string            // "" = memory-only
	file string            // dir/originals.jsonl when persisted
}

type jsonlRecord struct {
	Ref  string `json:"ref"`
	Text string `json:"text"`
}

// NewOriginals returns a memory-only store.
func NewOriginals() *Originals {
	return &Originals{m: make(map[string]string)}
}

// OpenOriginals returns a store persisted to <dir>/originals.jsonl, loading
// existing records. Directory is created if missing.
func OpenOriginals(dir string) (*Originals, error) {
	s := NewOriginals()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s.dir = dir
	s.file = filepath.Join(dir, "originals.jsonl")
	f, err := os.Open(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // fresh store
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024) // originals can be huge
	for sc.Scan() {
		var rec jsonlRecord
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Ref != "" {
			s.m[rec.Ref] = rec.Text
		}
	}
	return s, sc.Err()
}

// RefOf computes the content reference for an original text.
func RefOf(orig []byte) string {
	h := sha256.Sum256(orig)
	return hex.EncodeToString(h[:])
}

// Put stores an original and persists it when backed by a directory.
// Returns the content ref.
func (o *Originals) Put(orig []byte) (string, error) {
	ref := RefOf(orig)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.m[ref]; exists {
		return ref, nil // content-addressed: identical bytes already stored
	}
	o.m[ref] = string(orig)
	if o.file == "" {
		return ref, nil
	}
	f, err := os.OpenFile(o.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return ref, err
	}
	defer f.Close()
	rec, _ := json.Marshal(jsonlRecord{Ref: ref, Text: string(orig)})
	_, err = f.Write(append(rec, '\n'))
	return ref, err
}

// Get resolves a ref to the full original text.
func (o *Originals) Get(ref string) (string, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	t, ok := o.m[ref]
	return t, ok
}

// Resolve finds an original by a (possibly truncated) ref prefix as it may
// appear in markers typed by humans or models. Ambiguous prefixes error.
func (o *Originals) Resolve(prefix string) (string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	o.mu.RLock()
	defer o.mu.RUnlock()
	if t, ok := o.m[prefix]; ok {
		return t, nil
	}
	hit := ""
	found := false
	for ref, t := range o.m {
		if strings.HasPrefix(ref, prefix) {
			if found && t != hit {
				return "", errAmbiguous
			}
			hit, found = t, true
		}
	}
	if !found {
		return "", os.ErrNotExist
	}
	return hit, nil
}

var errAmbiguous = errString("ref prefix matches multiple originals")

type errString string

func (e errString) Error() string { return string(e) }
