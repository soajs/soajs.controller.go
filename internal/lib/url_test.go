package lib

import (
	"testing"
)

func TestParseServiceURL(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantService    string
		wantVersion    string
		wantPath       string
	}{
		{
			name:        "simple service",
			input:       "/urac/user/get",
			wantService: "urac",
			wantVersion: "1",
			wantPath:    "/user/get",
		},
		{
			name:        "service with version",
			input:       "/urac/v2/user/get",
			wantService: "urac",
			wantVersion: "2",
			wantPath:    "/user/get",
		},
		{
			name:        "service only",
			input:       "/controller",
			wantService: "controller",
			wantVersion: "1",
			wantPath:    "",
		},
		{
			name:        "empty path",
			input:       "/",
			wantService: "",
			wantVersion: "1",
			wantPath:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseServiceURL(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if info.ServiceName != tt.wantService {
				t.Errorf("ServiceName = %q, want %q", info.ServiceName, tt.wantService)
			}
			if info.ServiceVersion != tt.wantVersion {
				t.Errorf("ServiceVersion = %q, want %q", info.ServiceVersion, tt.wantVersion)
			}
			if info.ServicePath != tt.wantPath {
				t.Errorf("ServicePath = %q, want %q", info.ServicePath, tt.wantPath)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo/bar", "/foo/bar"},
		{"/foo/bar/", "/foo/bar"},
		{"//foo//bar//", "/foo/bar"},
		{"/", "/"},
		{"", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizePath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsVersionPattern(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"v1", true},
		{"v2", true},
		{"v10", true},
		{"v123", true},
		{"v", false},
		{"1", false},
		{"version1", false},
		{"V1", false},
		{"v1a", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isVersionPattern(tt.input)
			if got != tt.want {
				t.Errorf("isVersionPattern(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
