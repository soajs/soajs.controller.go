package lib

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/whatwg.json was recorded from Node.js: new URL(target, "http://localhost").
func TestRequestURLParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/whatwg.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		In       string `json:"in"`
		Pathname string `json:"pathname"`
		Search   string `json:"search"`
		Error    bool   `json:"error"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		p, s, err := RequestURL(c.In)
		if c.Error {
			if err == nil {
				t.Errorf("%q: want an invalid URL, got %q %q", c.In, p, s)
			}
			continue
		}
		if err != nil || p != c.Pathname || s != c.Search {
			t.Errorf("%q: got %q %q %v, want %q %q", c.In, p, s, err, c.Pathname, c.Search)
		}
	}
}
