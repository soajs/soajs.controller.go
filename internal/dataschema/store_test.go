package dataschema

import (
	"fmt"
	"testing"
	"time"
)

// Findings of documents read per request expire and are capped; loaded collections are reset by
// a full load.
func TestStoreBounds(t *testing.T) {
	s := NewStore()
	clock := time.Unix(0, 0)
	s.now = func() time.Time { return clock }
	f := func(coll, id string) []Finding {
		return []Finding{{Collection: coll, ID: id, Path: "/x", Action: IgnoredField}}
	}
	for i := 0; i < PerRequestMax+10; i++ {
		clock = clock.Add(time.Second)
		s.Report(nil, "oauth_token", fmt.Sprint(i), f("oauth_token", fmt.Sprint(i)))
	}
	if n := len(s.List()); n != PerRequestMax {
		t.Errorf("capped at %d: %d", PerRequestMax, n)
	}
	if got := s.List(); got[0].ID == "0" {
		t.Error("the oldest are dropped first")
	}
	s.Report(nil, "tenants", "a", f("tenants", "a"))
	s.ReportData(nil, "users", "u", f("users", "u"))
	clock = clock.Add(PerRequestTTL + time.Second)
	if got := s.List(); len(got) != 1 || got[0].Collection != "tenants" {
		t.Errorf("per-request findings expire, loaded ones stay: %+v", got)
	}
	s.BeginLoad("tenants")
	s.EndLoad("tenants")
	if len(s.List()) != 0 {
		t.Error("a full load that does not read a document forgets it")
	}
}
