package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
)

// /schemaProblems lists what the gateway found in the documents it read, by collection.
func TestSchemaProblemsRoute(t *testing.T) {
	saved := dataschema.Problems
	defer func() { dataschema.Problems = saved }()
	dataschema.Problems = dataschema.NewStore()
	dataschema.Problems.Report(zap.NewNop(), "tenants", "6a0e00000000000000000003", []dataschema.Finding{
		{Collection: "tenants", ID: "6a0e00000000000000000003", Path: "/applications/0/keys/0/extKeys/0/device", Rule: "/x", Action: dataschema.DroppedPiece, Piece: "/applications/0/keys/0/extKeys/0"},
		{Collection: "tenants", ID: "6a0e00000000000000000003", Path: "/extra", Rule: "/additionalProperties", Action: dataschema.IgnoredField},
	})
	dataschema.Problems.Report(zap.NewNop(), "products", "6a0e000000000000000000b1", []dataschema.Finding{
		{Collection: "products", ID: "6a0e000000000000000000b1", Path: "/packages/0/acl/dev/svc", Rule: "keyACL", Action: dataschema.OldShape},
	})
	h := &maintenanceHandler{logger: zap.NewNop()}
	for query, want := range map[string]int{"": 3, "?collection=tenants": 2, "?collection=users": 0} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/schemaProblems"+query, nil))
		var resp struct {
			Result bool `json:"result"`
			Data   struct {
				Documents int                  `json:"documents"`
				Actions   map[string]int       `json:"actions"`
				Problems  []dataschema.Finding `json:"problems"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || !resp.Result || len(resp.Data.Problems) != want {
			t.Errorf("%q: %d %s", query, rec.Code, rec.Body.String())
		}
		if query == "" && (resp.Data.Documents != 2 || resp.Data.Actions["old shape"] != 1 || resp.Data.Problems[0].Collection != "products") {
			t.Errorf("summary: %s", rec.Body.String())
		}
	}
	// a document read again without problems is forgotten
	dataschema.Problems.Report(zap.NewNop(), "products", "6a0e000000000000000000b1", nil)
	if len(dataschema.Problems.List()) != 2 {
		t.Error("a clean document leaves the list")
	}
}
