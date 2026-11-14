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

func TestParseAbsoluteURL(t *testing.T) {
	cases := []struct {
		in   string
		want *AbsoluteURL
	}{
		{"http://127.0.0.1:24100/public/hello?x=1#f", &AbsoluteURL{"http", "127.0.0.1", "24100", "/public/hello"}},
		{" HTTPS://User@Example.COM:443/a/./b/../c", &AbsoluteURL{"https", "example.com", "", "/a/c"}},
		{"http://[::1]:80", &AbsoluteURL{"http", "[::1]", "", "/"}},
		{"http:\\\\host\\a b", &AbsoluteURL{"http", "host", "", "/a%20b"}},
		{"http://:::", nil},
		{"/conform/v1/public/hello", nil},
		{"ftp://host/x", nil},
		{"http://host:99999/", nil},
	}
	for _, c := range cases {
		got, err := ParseAbsoluteURL(c.in)
		if c.want == nil {
			if err == nil {
				t.Errorf("%q: %+v, want an error", c.in, got)
			}
			continue
		}
		if err != nil || *got != *c.want {
			t.Errorf("%q: %+v %v, want %+v", c.in, got, err, c.want)
		}
	}
}
