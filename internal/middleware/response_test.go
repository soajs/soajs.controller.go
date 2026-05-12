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

func TestResponseWriter_Write(t *testing.T) {
	recorder := httptest.NewRecorder()
	rw := &ResponseWriter{ResponseWriter: recorder}

	data := []byte("hello world")
	n, err := rw.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if n != len(data) {
		t.Errorf("Write() returned %d, want %d", n, len(data))
	}

	if rw.Size() != len(data) {
		t.Errorf("Size() = %d, want %d", rw.Size(), len(data))
	}

	if !rw.Written() {
		t.Error("Written() = false, want true")
	}

	// Default status should be OK
	if rw.StatusCode() != http.StatusOK {
		t.Errorf("StatusCode() = %d, want %d", rw.StatusCode(), http.StatusOK)
	}
}

func TestNewSOAJSError(t *testing.T) {
	err := NewSOAJSError(150, "Test error")

	if len(err.Codes) != 1 {
		t.Errorf("Codes length = %d, want 1", len(err.Codes))
	}

	if err.Codes[0] != 150 {
		t.Errorf("Codes[0] = %d, want 150", err.Codes[0])
	}

	if len(err.Details) != 1 {
		t.Errorf("Details length = %d, want 1", len(err.Details))
	}

	if err.Details[0].Code != 150 {
		t.Errorf("Details[0].Code = %d, want 150", err.Details[0].Code)
	}

	if err.Details[0].Message != "Test error" {
		t.Errorf("Details[0].Message = %s, want 'Test error'", err.Details[0].Message)
	}
}

func TestSOAJSError_AddError(t *testing.T) {
	err := NewSOAJSError(150, "First error")
	err.AddError(151, "Second error")

	if len(err.Codes) != 2 {
		t.Errorf("Codes length = %d, want 2", len(err.Codes))
	}

	if err.Codes[1] != 151 {
		t.Errorf("Codes[1] = %d, want 151", err.Codes[1])
	}

	if len(err.Details) != 2 {
		t.Errorf("Details length = %d, want 2", len(err.Details))
	}
}

func TestBuildResponse_Success(t *testing.T) {
	data := map[string]string{"key": "value"}
	resp := BuildResponse(data, nil)

	if !resp.Result {
		t.Error("Result = false, want true")
	}

	if resp.Errors != nil {
		t.Error("Errors should be nil for success response")
	}
}

func TestBuildResponse_Error(t *testing.T) {
	err := NewSOAJSError(150, "Test error")
	resp := BuildResponse(nil, err)

	if resp.Result {
		t.Error("Result = true, want false")
	}

	if resp.Errors == nil {
		t.Error("Errors should not be nil for error response")
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	err := WriteJSON(w, http.StatusCreated, data)
	if err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}
}

func TestWriteJSONWithHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	headers := map[string]string{
		"X-Custom-Header": "custom-value",
	}

	err := WriteJSONWithHeaders(w, http.StatusOK, headers, data)
	if err != nil {
		t.Fatalf("WriteJSONWithHeaders() error = %v", err)
	}

	if customHeader := w.Header().Get("X-Custom-Header"); customHeader != "custom-value" {
		t.Errorf("X-Custom-Header = %s, want custom-value", customHeader)
	}
}

func TestWriteErrors(t *testing.T) {
	w := httptest.NewRecorder()
	errors := []ErrorDetail{
		{Code: 150, Message: "Error 1"},
		{Code: 151, Message: "Error 2"},
	}

	err := WriteErrors(w, http.StatusBadRequest, errors)
	if err != nil {
		t.Fatalf("WriteErrors() error = %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp SOAJSResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Result {
		t.Error("Result = true, want false")
	}

	if len(resp.Errors.Codes) != 2 {
		t.Errorf("Errors.Codes length = %d, want 2", len(resp.Errors.Codes))
	}
}

func TestResponse_Middleware(t *testing.T) {
	handler := Response()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}
