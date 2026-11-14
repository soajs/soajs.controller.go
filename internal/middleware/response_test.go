package middleware

import (
	"encoding/json"
	"fmt"
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

	if len(err.Codes) != 1 || err.Codes[0] != 150 {
		t.Errorf("Codes = %v, want [150]", err.Codes)
	}
	if len(err.Details) != 1 || err.Details[0].Code != 150 || err.Details[0].Message != "Test error" {
		t.Errorf("Details = %v, want [{150 Test error}]", err.Details)
	}
}

// TestSOAJSError_AddError follows addErrorCode in the Node.js response.js.
func TestSOAJSError_AddError(t *testing.T) {
	err := NewSOAJSError(154, " First error ")
	err.AddError(1000, "Second error")
	err.AddError(20, "")
	err.AddError(154, "Duplicate")

	// JavaScript's default sort compares the codes as text
	if got, want := fmt.Sprint(err.Codes), "[1000 154 20]"; got != want {
		t.Errorf("Codes = %s, want %s", got, want)
	}
	// details keep the order they were added in, trimmed, and none for an empty message
	if got, want := fmt.Sprint(err.Details), "[{154 First error} {1000 Second error}]"; got != want {
		t.Errorf("Details = %s, want %s", got, want)
	}
}

// TestEnvelopeBytes checks the exact bytes of the Node.js gateway answers.
func TestEnvelopeBytes(t *testing.T) {
	tests := []struct {
		name   string
		write  func(w http.ResponseWriter)
		status int
		body   string
	}{
		{
			name:   "code with message and status",
			write:  func(w http.ResponseWriter) { WriteCode(w, 132) },
			status: 403,
			body:   `{"result":false,"errors":{"codes":[132],"details":[{"code":132,"message":"A valid key is needed to access any API."}]}}`,
		},
		{
			name:   "code without status answers 500",
			write:  func(w http.ResponseWriter) { WriteCode(w, 147) },
			status: 500,
			body:   `{"result":false,"errors":{"codes":[147],"details":[{"code":147,"message":"Token restriction mismatch"}]}}`,
		},
		{
			name:   "code without message",
			write:  func(w http.ResponseWriter) { WriteCode(w, 999) },
			status: 500,
			body:   `{"code":999,"status":500}`,
		},
		{
			name:   "code 136 answers 200",
			write:  func(w http.ResponseWriter) { WriteCode(w, 136) },
			status: 200,
			body:   `{"result":false,"errors":{"codes":[136],"details":[{"code":136,"message":"Hello I am here! what do you want me to do!"}]}}`,
		},
		{
			name:   "no html escaping",
			write:  func(w http.ResponseWriter) { WriteError(w, 400, 400, "a <b> & c") },
			status: 400,
			body:   `{"result":false,"errors":{"codes":[400],"details":[{"code":400,"message":"a <b> & c"}]}}`,
		},
		{
			name:   "banned",
			write:  func(w http.ResponseWriter) { WriteStatusMsg(w, 403, "banned", nil, false) },
			status: 403,
			body:   `{"status":403,"msg":"banned"}`,
		},
		{
			name:   "maintenance without retry",
			write:  func(w http.ResponseWriter) { WriteStatusMsg(w, 503, "Maintenance mode is on, come back soon", nil, true) },
			status: 503,
			body:   `{"status":503,"msg":"Maintenance mode is on, come back soon","headObj":null}`,
		},
		{
			name:   "success",
			write:  func(w http.ResponseWriter) { WriteSuccess(w, map[string]string{"a": "b"}) },
			status: 200,
			body:   `{"result":true,"data":{"a":"b"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.write(w)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if got := w.Body.String(); got != tt.body {
				t.Errorf("body = %s, want %s", got, tt.body)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

func TestWriteStatusMsgHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	WriteStatusMsg(w, 503, "down", map[string]string{"Retry-After": "120"}, false)
	if got := w.Header().Get("Retry-After"); got != "120" {
		t.Errorf("Retry-After = %q, want 120", got)
	}
	if got := w.Body.String(); got != `{"status":503,"msg":"down"}` {
		t.Errorf("body = %s", got)
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
