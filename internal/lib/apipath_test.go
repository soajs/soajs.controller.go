package lib

import (
	"testing"
)

func TestAPIPathToRegex(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{"/user/:id", "/user/123", true},
		{"/user/:id", "/user/abc", true},
		{"/user/:id", "/user/", false},
		{"/user/:id/profile", "/user/123/profile", true},
		{"/api/*", "/api/anything/here", true},
		{"/exact/path", "/exact/path", true},
		{"/exact/path", "/exact/path/more", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"->"+tt.path, func(t *testing.T) {
			match, _ := MatchPath(tt.pattern, tt.path)
			if match != tt.match {
				t.Errorf("MatchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, match, tt.match)
			}
		})
	}
}

func TestExtractPathParams(t *testing.T) {
	params := ExtractPathParams("/user/:id/post/:postId", "/user/123/post/456")

	if params["id"] != "123" {
		t.Errorf("params[id] = %q, want %q", params["id"], "123")
	}
	if params["postId"] != "456" {
		t.Errorf("params[postId] = %q, want %q", params["postId"], "456")
	}
}

func TestNormalizeAPIPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/foo/bar/", "/foo/bar"},
		{"foo/bar", "/foo/bar"},
		{"/", "/"},
		{"", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeAPIPath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeAPIPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
