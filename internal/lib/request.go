package lib

import (
	"bytes"
	"io"
	"net/http"
)

// CloneRequest creates a shallow copy of the request with a new context
func CloneRequest(r *http.Request) *http.Request {
	r2 := r.Clone(r.Context())
	return r2
}

// CloneRequestWithBody clones the request and reads/restores the body
func CloneRequestWithBody(r *http.Request) (*http.Request, error) {
	if r.Body == nil {
		return CloneRequest(r), nil
	}

	// Read the body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	// Restore original body
	r.Body = io.NopCloser(bytes.NewReader(body))

	// Clone and set body
	r2 := CloneRequest(r)
	r2.Body = io.NopCloser(bytes.NewReader(body))

	return r2, nil
}

// DrainBody reads and discards the request body
func DrainBody(r *http.Request) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
}

// GetContentType returns the content type without parameters
func GetContentType(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return ""
	}

	// Strip parameters
	for i, c := range ct {
		if c == ';' {
			return ct[:i]
		}
	}
	return ct
}

// IsJSONRequest checks if the request has JSON content type
func IsJSONRequest(r *http.Request) bool {
	ct := GetContentType(r)
	return ct == "application/json" || ct == "text/json"
}

// IsFormRequest checks if the request has form content type
func IsFormRequest(r *http.Request) bool {
	ct := GetContentType(r)
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}
