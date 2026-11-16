// Package conformance replays a fixed set of requests against a running gateway and compares each
// answer with the one the Node.js gateway gave, byte for byte.
//
// The answers of the Node.js gateway are recorded in golden/. Both gateways run against the same
// fixture (see fixture/), and run.sh starts either one. The cases are in cases.json; a case the Go
// gateway does not pass yet is listed in pending.json with the pull request that ports it.
//
//	CONFORMANCE_IMPL=go|node       which gateway is running
//	CONFORMANCE_GATEWAY=<url>      its gateway port, e.g. http://127.0.0.1:24000
//	CONFORMANCE_MAINTENANCE=<url>  its maintenance port, e.g. http://127.0.0.1:25000
//	CONFORMANCE_DASHBOARD_GATEWAY=<url>      the gateway of the dashboard environment, for the
//	CONFORMANCE_DASHBOARD_MAINTENANCE=<url>  cases with "env": "dashboard"
//	CONFORMANCE_MONGO_URI=<uri>              the call events the dashboard gateway writes
//
// go test ./test/conformance -record writes golden/ from the running gateway, which must be Node.js.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var record = flag.Bool("record", false, "write golden/ from the running gateway (Node.js only)")

// vars are the fixture values a case refers to as $NAME.
var vars = map[string]string{
	"KEY_DEFAULT":        "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609ac1ebf7aecfdc2355849a594cc55d042d9906c2c0190a3b7d5f5935d8053a953ede02160a6f8429f1ba971b8d9d6fe6afe7c59af3462d188c4b1a08e9c07670e",
	"KEY_EXPIRED":        "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d2760944be5f2421ce4cd2c35aae1da35ac3b96a72d5942c01956d1a29a29cfee5fa6d1c1e0bffdd48b418fb8615150a38a5c35b6117a9a86bcfdcf6086c797ace797a",
	"KEY_STG":            "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609ae1f75a4f8cdf2c4d66a956a94354d315cb54fffd2da740b16b23321cc397e7aee083b3aea2b66b992166c72629cfcad4b36c5c5b989de68c54a7b31cb01812e",
	"KEY_DEVICE_ALLOW":   "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d276090161d2ca4e8c4f1f7674012c0f9951162a40214185d3bef8910b61fc9961940a35c240d4bae7ef93469c4dcc42719d8347dfa6fe078b9b4c91e247d69c0ce49d",
	"KEY_DEVICE_DENY":    "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609856bbdf663aba70a17267c46c56626b7f5e73acfe511e279630c82efaadbaf3a0b55c38bbcdf702cd841312cdd2afa3dae8289de9bcc029f15090ef61841ec7e",
	"KEY_DEVICE_INVALID": "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609feae4af9fb2356a6cc6e7b1680e6531f95480d462ea6ffbe166a67f1a80e1dcd7568954446f89aeedf7fe019d27812ba5a112e2cdfa975daf59762cbae576fb6",
	"KEY_NOSVC":          "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d2760947f606f0000f7253607e4e33904ca3b83f6f77de03e310b91c80560ba80dd8dd623e129efcf0892b36ab66f32795fd06ab9121ca5d2674be1494591a2ae3f4f3",
	"KEY_EMPTY_RULES":    "5bbf311a30f4acaec52b7472be854cc9866dc9aaa2a3f7e1dd160e28420fb37dd51951b7363350bc0eeaa974cfdd27cfb4e908a20c4d343dffe53def7de35480a4355ba0bb131925b79f4f3d97453340e997da812c0eb3b4c3dd31cc7b88ff64",
	"TOKEN_VALID":        "c0f0000000000000000000000000000000000001",
	"TOKEN_EXPIRED":      "c0f0000000000000000000000000000000000002",
	// agent tokens: restricted to KEY_DEFAULT, to [KEY_DEFAULT], and to KEY_STG (no name is the
	// start of another, expand replaces them in any order)
	"TOKEN_AGENT_KEY":  "a9e0000000000000000000000000000000000001",
	"TOKEN_LIST_AGENT": "a9e0000000000000000000000000000000000002",
	"TOKEN_STG_AGENT":  "a9e0000000000000000000000000000000000003",
	// the console key and urac tokens of the dashboard environment
	"KEY_CONSOLE":      "5bbf311a30f4acaec52b7472be854cc92a688050ab8b4c65b32000cbc9699d1d3a106575479749cdb84c50721a91114c6b37466822a73afe81da0776a62d7660f95628343b1423d438eef3b40607777da2c6028b062e623f33ce704ed2d6c2fabfc8ce466e1a8887e8f9c16a566ca64c",
	"TOKEN_U_OWNER":    "d0a0000000000000000000000000000000000001",
	"TOKEN_U_PAUSED":   "d0a0000000000000000000000000000000000002",
	"TOKEN_U_PENDING":  "d0a0000000000000000000000000000000000003",
	"TOKEN_U_GHOST":    "d0a0000000000000000000000000000000000004",
	"TOKEN_U_OUTSIDER": "d0a0000000000000000000000000000000000005",
	"TOKEN_U_INVITED":  "d0a0000000000000000000000000000000000006",
	"TOKEN_U_MOVED":    "d0a0000000000000000000000000000000000007",
	// dashboard tokens in the dev token database: roaming
	"TOKEN_R_ROAMING": "d0a0000000000000000000000000000000000008",
	"TOKEN_R_GONE":    "d0a0000000000000000000000000000000000009",
	// a urac token of the dev environment, which has no urac masterDB
	"TOKEN_U_DEV": "d0a000000000000000000000000000000000000a",
}

// Case is one request.
type Case struct {
	Name   string            `json:"name"`
	About  string            `json:"about,omitempty"`
	Target string            `json:"target,omitempty"` // "gateway" (default) or "maintenance"
	Env    string            `json:"env,omitempty"`    // "dev" (default) or "dashboard"
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"headers,omitempty"`
	Body   string            `json:"body,omitempty"`
	// CompareHeaders are compared besides Content-Type.
	CompareHeaders []string `json:"compareHeaders,omitempty"`
	// IgnoreFields are JSON fields whose value changes on every call, such as a timestamp.
	IgnoreFields []string `json:"ignoreFields,omitempty"`
	// CompareJSON compares the bodies as JSON values instead of bytes, for answers whose key
	// order follows a map, such as a registry.
	CompareJSON bool `json:"compareJSON,omitempty"`
	// DropFields are keys removed, at any depth, from both bodies before a JSON compare: values
	// a gateway deliberately leaves out.
	DropFields []string `json:"dropFields,omitempty"`
	// SortArrays sorts the arrays of strings of both bodies before a JSON compare: lists whose
	// order follows a map in Go and the document in Node.js.
	SortArrays bool `json:"sortArrays,omitempty"`
}

// Answer is what a gateway answered.
type Answer struct {
	Status int               `json:"status"`
	Header map[string]string `json:"headers"`
	Body   string            `json:"body"`
}

func TestConformance(t *testing.T) {
	impl := os.Getenv("CONFORMANCE_IMPL")
	gateway := os.Getenv("CONFORMANCE_GATEWAY")
	maintenance := os.Getenv("CONFORMANCE_MAINTENANCE")
	dashboardGateway := os.Getenv("CONFORMANCE_DASHBOARD_GATEWAY")
	dashboardMaintenance := os.Getenv("CONFORMANCE_DASHBOARD_MAINTENANCE")
	if impl == "" || gateway == "" || maintenance == "" {
		t.Skip("set CONFORMANCE_IMPL, CONFORMANCE_GATEWAY and CONFORMANCE_MAINTENANCE, or use run.sh")
	}
	if *record && impl != "node" {
		t.Fatal("golden answers are recorded from the Node.js gateway only")
	}

	var cases []Case
	readJSON(t, "cases.json", &cases)
	pending := map[string]string{}
	if impl == "go" {
		readJSON(t, "pending.json", &pending)
	}
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			t.Fatalf("duplicate case %q", c.Name)
		}
		names[c.Name] = true
	}
	for name := range pending {
		if !names[name] {
			t.Errorf("pending.json names an unknown case %q", name)
		}
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			base := gateway
			if c.Target == "maintenance" {
				base = maintenance
			}
			if c.Env == "dashboard" {
				if dashboardGateway == "" || dashboardMaintenance == "" {
					t.Fatal("set CONFORMANCE_DASHBOARD_GATEWAY and CONFORMANCE_DASHBOARD_MAINTENANCE, or use run.sh")
				}
				base = dashboardGateway
				if c.Target == "maintenance" {
					base = dashboardMaintenance
				}
			}
			got, err := send(client, base, c)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("golden", c.Name+".json")
			if *record {
				writeJSON(t, golden, got)
				return
			}
			var want Answer
			readJSON(t, golden, &want)
			diff := compare(c, want, got)
			reason, isPending := pending[c.Name]
			switch {
			case isPending && diff == "":
				t.Errorf("case passes now, remove it from pending.json (%s)", reason)
			case isPending:
				t.Skipf("pending: %s\n%s", reason, diff)
			case diff != "":
				t.Error(diff)
			}
		})
	}

	// the call events the dashboard gateway wrote (events are on there), compared with Node.js's
	t.Run("call_events", func(t *testing.T) {
		uri := os.Getenv("CONFORMANCE_MONGO_URI")
		if uri == "" {
			t.Skip("set CONFORMANCE_MONGO_URI, or use run.sh")
		}
		got := callEvents(t, uri)
		golden := filepath.Join("golden", "_call_events.json")
		if *record {
			writeJSON(t, golden, got)
			return
		}
		var want []json.RawMessage
		readJSON(t, golden, &want)
		compact := func(list []json.RawMessage) map[string]int {
			out := map[string]int{}
			for _, raw := range list {
				var b bytes.Buffer
				if err := json.Compact(&b, raw); err != nil {
					t.Fatal(err)
				}
				out[b.String()]++
			}
			return out
		}
		wantSet, gotSet := compact(want), compact(got)
		for doc, n := range wantSet {
			if gotSet[doc] != n {
				t.Errorf("Node.js wrote %d, Go %d of:\n  %s", n, gotSet[doc], doc)
			}
		}
		for doc, n := range gotSet {
			if _, ok := wantSet[doc]; !ok {
				t.Errorf("Go wrote %d that Node.js did not:\n  %s", n, doc)
			}
		}
	})
}

func send(client *http.Client, base string, c Case) (Answer, error) {
	var body io.Reader
	if c.Body != "" {
		body = strings.NewReader(expand(c.Body))
	}
	req, err := http.NewRequest(c.Method, base+expand(c.Path), body)
	if err != nil {
		return Answer{}, err
	}
	for k, v := range c.Header {
		req.Header.Set(k, expand(v))
	}
	resp, err := client.Do(req)
	if err != nil {
		return Answer{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Answer{}, err
	}
	a := Answer{Status: resp.StatusCode, Header: map[string]string{}, Body: string(raw)}
	for _, h := range headerNames(c) {
		if v := resp.Header.Values(h); len(v) > 0 {
			a.Header[http.CanonicalHeaderKey(h)] = strings.Join(v, ", ")
		}
	}
	return a, nil
}

func compare(c Case, want, got Answer) string {
	var diffs []string
	if want.Status != got.Status {
		diffs = append(diffs, fmt.Sprintf("status: want %d, got %d", want.Status, got.Status))
	}
	for _, h := range headerNames(c) {
		k := http.CanonicalHeaderKey(h)
		if want.Header[k] != got.Header[k] {
			diffs = append(diffs, fmt.Sprintf("header %s: want %q, got %q", k, want.Header[k], got.Header[k]))
		}
	}
	wantBody, gotBody := mask(want.Body, c.IgnoreFields), mask(got.Body, c.IgnoreFields)
	// each gateway reports its own version on the maintenance port
	wantBody, gotBody = serviceVersion.ReplaceAllString(wantBody, "${1}null"), serviceVersion.ReplaceAllString(gotBody, "${1}null")
	if c.CompareJSON {
		var w, g interface{}
		if json.Unmarshal([]byte(wantBody), &w) == nil && json.Unmarshal([]byte(gotBody), &g) == nil && reflect.DeepEqual(normalize(w, c), normalize(g, c)) {
			gotBody = wantBody
		}
	}
	if wantBody != gotBody {
		diffs = append(diffs, fmt.Sprintf("body:\n  want %s\n  got  %s", wantBody, gotBody))
	}
	return strings.Join(diffs, "\n")
}

func headerNames(c Case) []string {
	names := append([]string{"Content-Type"}, c.CompareHeaders...)
	sort.Strings(names)
	return names
}

// drop removes the keys, at any depth.
// normalize applies a case's dropFields and sortArrays to a JSON body.
func normalize(v interface{}, c Case) interface{} {
	v = drop(v, c.DropFields)
	if c.SortArrays {
		v = sortStrings(v)
	}
	return v
}

// sortStrings sorts, at any depth, the arrays whose items are all strings.
func sortStrings(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, e := range t {
			t[k] = sortStrings(e)
		}
	case []interface{}:
		strs := make([]string, 0, len(t))
		for i, e := range t {
			t[i] = sortStrings(e)
			if s, ok := e.(string); ok {
				strs = append(strs, s)
			}
		}
		if len(strs) == len(t) {
			sort.Strings(strs)
			for i, s := range strs {
				t[i] = s
			}
		}
	}
	return v
}

func drop(v interface{}, keys []string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for _, k := range keys {
			delete(t, k)
		}
		for k, e := range t {
			t[k] = drop(e, keys)
		}
	case []interface{}:
		for i, e := range t {
			t[i] = drop(e, keys)
		}
	}
	return v
}

// serviceVersion is the version of the gateway in a maintenance answer's service block.
var serviceVersion = regexp.MustCompile(`("service":\{"service":"soajs\.controller","version":)"[^"]*"`)

// mask replaces the value of each ignored field, a JSON number or string, with null.
func mask(body string, fields []string) string {
	for _, f := range fields {
		re := regexp.MustCompile(`("` + regexp.QuoteMeta(f) + `":)(-?[0-9.eE+-]+|"(?:[^"\\]|\\.)*")`)
		body = re.ReplaceAllString(body, "${1}null")
	}
	return body
}

func expand(s string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "$"+k, v)
	}
	return s
}

func readJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// eventsMasked are the call event fields that change on every run.
var eventsMasked = []string{"_id", "ts", "durationMs", "requestId"}

// callEvents reads conformance_gateway.call_events once the gateway stopped writing (no new event
// for 2.5 s, at most 20 s): each document without the masked fields, as canonical extended JSON
// with sorted keys, so field types (Int32, Long, Double) are compared too; sorted.
func callEvents(t *testing.T, uri string) []json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	coll := client.Database("conformance_gateway").Collection("call_events")
	last, stableSince, deadline := int64(-1), time.Now(), time.Now().Add(20*time.Second)
	for time.Now().Before(deadline) {
		n, err := coll.CountDocuments(ctx, bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		if n != last {
			last, stableSince = n, time.Now()
		} else if time.Since(stableSince) >= 2500*time.Millisecond {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var doc bson.D
		if err := bson.Unmarshal(cur.Current, &doc); err != nil {
			t.Fatal(err)
		}
		kept := bson.D{}
		for _, e := range doc {
			masked := false
			for _, m := range eventsMasked {
				if e.Key == m {
					masked = true
				}
			}
			if !masked {
				kept = append(kept, e)
			}
		}
		ext, err := bson.MarshalExtJSON(kept, true, false)
		if err != nil {
			t.Fatal(err)
		}
		var v interface{}
		if err := json.Unmarshal(ext, &v); err != nil {
			t.Fatal(err)
		}
		sorted, _ := json.Marshal(v)
		out = append(out, string(sorted))
	}
	sort.Strings(out)
	raw := make([]json.RawMessage, len(out))
	for i, s := range out {
		raw[i] = json.RawMessage(s)
	}
	return raw
}
