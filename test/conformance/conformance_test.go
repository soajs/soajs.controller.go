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
//
// go test ./test/conformance -record writes golden/ from the running gateway, which must be Node.js.
package conformance

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var record = flag.Bool("record", false, "write golden/ from the running gateway (Node.js only)")

// vars are the fixture values a case refers to as $NAME.
var vars = map[string]string{
	"KEY_DEFAULT":   "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609ac1ebf7aecfdc2355849a594cc55d042d9906c2c0190a3b7d5f5935d8053a953ede02160a6f8429f1ba971b8d9d6fe6afe7c59af3462d188c4b1a08e9c07670e",
	"KEY_NOSVC":     "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d2760947f606f0000f7253607e4e33904ca3b83f6f77de03e310b91c80560ba80dd8dd623e129efcf0892b36ab66f32795fd06ab9121ca5d2674be1494591a2ae3f4f3",
	"TOKEN_VALID":   "c0f0000000000000000000000000000000000001",
	"TOKEN_EXPIRED": "c0f0000000000000000000000000000000000002",
}

// Case is one request.
type Case struct {
	Name   string            `json:"name"`
	About  string            `json:"about,omitempty"`
	Target string            `json:"target,omitempty"` // "gateway" (default) or "maintenance"
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"headers,omitempty"`
	Body   string            `json:"body,omitempty"`
	// CompareHeaders are compared besides Content-Type.
	CompareHeaders []string `json:"compareHeaders,omitempty"`
	// IgnoreFields are JSON fields whose value changes on every call, such as a timestamp.
	IgnoreFields []string `json:"ignoreFields,omitempty"`
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
