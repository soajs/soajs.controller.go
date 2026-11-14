package lib

import "testing"

func TestPathToRegexp(t *testing.T) {
	tests := []struct {
		route, path string
		match       bool
	}{
		{"/user/:id", "/user/123", true},
		{"/user/:id", "/user/123/", true},
		{"/user/:id", "/user/", false},
		{"/user/:id", "/user/1/2", false},
		{"/hooks/:hookId/:token", "/hooks/a/b", true},
		{"/files/*path", "/files/a/b/c", true},
		{"/item{/:id}", "/item", true},
		{"/item{/:id}", "/item/9", true},
		{"/Exact.path", "/Exact.path", true},
		{"/Exact.path", "/exact.path", false},
		{"/Exact.path", "/ExactXpath", false},
	}
	for _, tt := range tests {
		re, err := PathToRegexp(tt.route)
		if err != nil {
			t.Fatalf("%s: %v", tt.route, err)
		}
		if got := re.MatchString(tt.path); got != tt.match {
			t.Errorf("%s ~ %s = %v, want %v", tt.route, tt.path, got, tt.match)
		}
	}
}

