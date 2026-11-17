package dataschema

import (
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Store keeps the current findings of every document read, for the maintenance route
// /schemaProblems, and logs each document's findings at error once per load.
//
// Collections loaded in full (provision, registry) are reset at each load. The documents read per
// request (users, groups, tokens) and the data problems are kept for PerRequestTTL after their last
// read, and at most PerRequestMax of them, the oldest dropped first.
type Store struct {
	mu   sync.Mutex
	docs map[string]*storedDoc
	now  func() time.Time
}

// PerRequestTTL and PerRequestMax bound the findings of documents read per request.
const (
	PerRequestTTL = time.Hour
	PerRequestMax = 1000
)

// perRequest are the collections read per request rather than loaded.
var perRequest = map[string]bool{"users": true, "groups": true, "oauth_token": true}

type storedDoc struct {
	coll       string
	findings   []Finding
	perRequest bool
	seen       time.Time
	// stale is set when a full load of the collection begins and cleared when the document is
	// read again; a document still stale at the end of the load is no longer there.
	stale bool
}

// Problems is the gateway's store.
var Problems = NewStore()

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{docs: map[string]*storedDoc{}, now: time.Now}
}

func storeKey(coll, id string) string { return coll + "\x00" + id }

// BeginLoad marks the documents of the collections as not yet read by a full load.
func (s *Store) BeginLoad(colls ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if !d.perRequest && contains(colls, d.coll) {
			d.stale = true
		}
	}
}

// EndLoad forgets the documents of the collections the full load did not read.
func (s *Store) EndLoad(colls ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, d := range s.docs {
		if d.stale && contains(colls, d.coll) {
			delete(s.docs, k)
		}
	}
}

// Report replaces the findings of one document. They are logged at error when they are new or
// changed, or when a full load reads the document again; a document read without findings is
// forgotten.
func (s *Store) Report(logger *zap.Logger, coll, id string, findings []Finding) {
	s.report(logger, storeKey(coll, id), coll, id, perRequest[coll], findings)
}

// ReportResult replaces the findings of the document a Read gave.
func (s *Store) ReportResult(logger *zap.Logger, res Result) {
	s.Report(logger, res.Collection, res.ID, res.Findings)
}

// ReportData replaces the data problems found about one document: references to things that do
// not exist. They are kept apart from what reading the document found, as per request.
func (s *Store) ReportData(logger *zap.Logger, coll, id string, findings []Finding) {
	s.report(logger, storeKey(coll, id)+"\x00data", coll, id, true, findings)
}

func (s *Store) report(logger *zap.Logger, key, coll, id string, perReq bool, findings []Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.docs[key]
	if len(findings) == 0 {
		delete(s.docs, key)
		return
	}
	log := prev == nil || prev.stale || !sameFindings(prev.findings, findings)
	s.docs[key] = &storedDoc{coll: coll, findings: append([]Finding(nil), findings...), perRequest: perReq, seen: s.now()}
	if perReq {
		s.prune()
	}
	if log && logger != nil {
		logger.Error("document read with problems: see /schemaProblems",
			zap.String("collection", coll), zap.String("_id", id), zap.Any("problems", findings))
	}
}

// prune drops the per-request documents past their TTL, then the oldest over the cap.
func (s *Store) prune() {
	now := s.now()
	var kept []string
	for k, d := range s.docs {
		if !d.perRequest {
			continue
		}
		if now.Sub(d.seen) > PerRequestTTL {
			delete(s.docs, k)
			continue
		}
		kept = append(kept, k)
	}
	if len(kept) <= PerRequestMax {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return s.docs[kept[i]].seen.Before(s.docs[kept[j]].seen) })
	for _, k := range kept[:len(kept)-PerRequestMax] {
		delete(s.docs, k)
	}
}

// List returns every current finding, by collection, _id and path.
func (s *Store) List() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
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

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
