package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"message": "hello"}
	err := WriteSuccess(w, data)
	if err != nil {
		t.Fatalf("WriteSuccess() error = %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp SOAJSResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Result {
		t.Error("Result = false, want true")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()

	err := WriteError(w, http.StatusForbidden, 150, "Access denied")
	if err != nil {
		t.Fatalf("WriteError() error = %v", err)
	}

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp SOAJSResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Result {
		t.Error("Result = true, want false")
	}

	if resp.Errors == nil {
		t.Fatal("Errors is nil")
	}

	if len(resp.Errors.Codes) != 1 || resp.Errors.Codes[0] != 150 {
		t.Errorf("Errors.Codes = %v, want [150]", resp.Errors.Codes)
	}
}

func TestResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	rw := &ResponseWriter{ResponseWriter: recorder}

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	if rw.StatusCode() != http.StatusCreated {
		t.Errorf("StatusCode() = %d, want %d", rw.StatusCode(), http.StatusCreated)
	}

	// Test double WriteHeader (should be ignored)
	rw.WriteHeader(http.StatusOK)
	if rw.StatusCode() != http.StatusCreated {
		t.Errorf("StatusCode() after second WriteHeader = %d, want %d", rw.StatusCode(), http.StatusCreated)
	}
}
