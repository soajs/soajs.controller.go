package dataschema

import (
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Store keeps the current findings of every document read, for the maintenance route
// /schemaProblems, and logs each document's findings at error once per load.
type Store struct {
	mu   sync.Mutex
	docs map[string]*storedDoc
}

type storedDoc struct {
	findings []Finding
	// stale is set when a full load of the collection begins and cleared when the document is
	// read again; a document still stale at the end of the load is no longer there.
	stale bool
}

// Problems is the gateway's store.
var Problems = NewStore()

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{docs: map[string]*storedDoc{}}
}

func storeKey(coll, id string) string { return coll + "\x00" + id }

// BeginLoad marks the documents of the collections as not yet read by a full load.
func (s *Store) BeginLoad(colls ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		for _, c := range colls {
			if len(d.findings) > 0 && d.findings[0].Collection == c {
				d.stale = true
			}
		}
	}
}

// EndLoad forgets the documents of the collections the full load did not read.
func (s *Store) EndLoad(colls ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, d := range s.docs {
		for _, c := range colls {
			if d.stale && len(d.findings) > 0 && d.findings[0].Collection == c {
				delete(s.docs, k)
			}
		}
	}
}

// Report replaces the findings of one document. They are logged at error when they are new or
// changed, or when a full load reads the document again; a document read without findings is
// forgotten.
func (s *Store) Report(logger *zap.Logger, coll, id string, findings []Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storeKey(coll, id)
	prev := s.docs[key]
	if len(findings) == 0 {
		delete(s.docs, key)
		return
	}
	log := prev == nil || prev.stale || !sameFindings(prev.findings, findings)
	s.docs[key] = &storedDoc{findings: append([]Finding(nil), findings...)}
	if log && logger != nil {
		logger.Error("document read with problems: see /schemaProblems",
			zap.String("collection", coll), zap.String("_id", id), zap.Any("problems", findings))
	}
}

// List returns every current finding, by collection, _id and path.
func (s *Store) List() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Finding{}
	for _, d := range s.docs {
		out = append(out, d.findings...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Collection != out[j].Collection {
			return out[i].Collection < out[j].Collection
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func sameFindings(a, b []Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
