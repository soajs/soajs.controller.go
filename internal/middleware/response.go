package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/soajserr"
)

// ResponseWriter wraps http.ResponseWriter with additional functionality
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
	size       int
	mu         sync.Mutex
}

// WriteHeader captures the status code
func (rw *ResponseWriter) WriteHeader(code int) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if !rw.written {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

// Write captures that writing has occurred
func (rw *ResponseWriter) Write(b []byte) (int, error) {
	rw.mu.Lock()
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	rw.mu.Unlock()
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// StatusCode returns the status code
func (rw *ResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Size returns the number of bytes written
func (rw *ResponseWriter) Size() int {
	return rw.size
}

// Written returns whether the response has been written
func (rw *ResponseWriter) Written() bool {
	return rw.written
}

// SOAJSResponse is the response body of the Node.js gateway (mw/response/response.js): result, then
// data when there is any, then errors.
type SOAJSResponse struct {
	Result bool        `json:"result"`
	Data   interface{} `json:"data,omitempty"`
	Errors *SOAJSError `json:"errors,omitempty"`
}

// SOAJSError holds the error codes of a response and the details of those that have a message.
type SOAJSError struct {
	Codes   []int         `json:"codes"`
	Details []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail holds a single error detail
type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewSOAJSError creates a new SOAJS error
func NewSOAJSError(code int, message string) *SOAJSError {
	e := &SOAJSError{Codes: []int{}}
	e.AddError(code, message)
	return e
}

// AddError adds an error as response.js addErrorCode does: a code already present is ignored, the
// codes are kept sorted as JavaScript's default sort orders them (as text), and a detail is kept
// only for a message that is not empty once trimmed.
func (e *SOAJSError) AddError(code int, message string) {
	for _, c := range e.Codes {
		if c == code {
			return
		}
	}
	e.Codes = append(e.Codes, code)
	sort.SliceStable(e.Codes, func(i, j int) bool {
		return strconv.Itoa(e.Codes[i]) < strconv.Itoa(e.Codes[j])
	})
	if message = strings.TrimSpace(message); message != "" {
		e.Details = append(e.Details, ErrorDetail{Code: code, Message: message})
	}
}

// Response middleware wraps the response writer with helper methods
func Response() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &ResponseWriter{
				ResponseWriter: w,
			}
			next.ServeHTTP(rw, r)
		})
	}
}

// encodeJSON encodes as JSON.stringify does: no HTML escaping, no trailing newline.
func encodeJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	body, err := encodeJSON(data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

// WriteSuccess writes a successful SOAJS response
func WriteSuccess(w http.ResponseWriter, data interface{}) error {
	return WriteJSON(w, http.StatusOK, SOAJSResponse{
		Result: true,
		Data:   data,
	})
}

// WriteError writes an error SOAJS response
func WriteError(w http.ResponseWriter, status int, code int, message string) error {
	noteCode(w, code)
	return WriteJSON(w, status, SOAJSResponse{
		Result: false,
		Errors: NewSOAJSError(code, message),
	})
}

// WriteCode answers with an error code as the Node.js gateway's error handler does: the message
// and status of the code, 500 when the code has no status. A code without a message is answered
// as the bare object {"code":...,"status":...}.
func WriteCode(w http.ResponseWriter, code int) error {
	noteCode(w, code)
	e := soajserr.Get(code)
	status := e.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if e.Msg == "" {
		return WriteJSON(w, status, struct {
			Code   int `json:"code"`
			Status int `json:"status"`
		}{code, status})
	}
	return WriteError(w, status, code, e.Msg)
}

// WriteStatusMsg answers with a bare {"status":...,"msg":...} body, as the Node.js gateway does for
// maintenance mode, a banned IP and throttling. headObj becomes response headers. A null headObj in
// Node.js stays in the body as "headObj":null, which nullHeadObj reproduces.
func WriteStatusMsg(w http.ResponseWriter, status int, msg string, headObj map[string]string, nullHeadObj bool) error {
	for k, v := range headObj {
		w.Header().Set(k, v)
	}
	if status == 0 {
		status = http.StatusOK
	}
	if nullHeadObj {
		return WriteJSON(w, status, struct {
			Status  int         `json:"status"`
			Msg     string      `json:"msg"`
			HeadObj interface{} `json:"headObj"`
		}{status, msg, nil})
	}
	return WriteJSON(w, status, struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}{status, msg})
}

// ResponseMetrics holds response timing metrics
type ResponseMetrics struct {
	StartTime    time.Time
	Duration     time.Duration
	StatusCode   int
	ResponseSize int
}

// CaptureMetrics captures response metrics from a ResponseWriter
func CaptureMetrics(rw *ResponseWriter, startTime time.Time) *ResponseMetrics {
	return &ResponseMetrics{
		StartTime:    startTime,
		Duration:     time.Since(startTime),
		StatusCode:   rw.StatusCode(),
		ResponseSize: rw.Size(),
	}
}
