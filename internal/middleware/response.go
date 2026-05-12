package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
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

// SOAJSResponse represents a standard SOAJS response
type SOAJSResponse struct {
	Result bool        `json:"result"`
	Data   interface{} `json:"data,omitempty"`
	Errors *SOAJSError `json:"errors,omitempty"`
}

// SOAJSError represents a SOAJS error
type SOAJSError struct {
	Codes   []int         `json:"codes"`
	Details []ErrorDetail `json:"details"`
}

// ErrorDetail holds a single error detail
type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewSOAJSError creates a new SOAJS error
func NewSOAJSError(code int, message string) *SOAJSError {
	return &SOAJSError{
		Codes: []int{code},
		Details: []ErrorDetail{
			{Code: code, Message: message},
		},
	}
}

// AddError adds an error to the SOAJS error
func (e *SOAJSError) AddError(code int, message string) {
	e.Codes = append(e.Codes, code)
	e.Details = append(e.Details, ErrorDetail{Code: code, Message: message})
}

// BuildResponse creates a SOAJS response from data or error
func BuildResponse(data interface{}, err *SOAJSError) *SOAJSResponse {
	if err != nil {
		return &SOAJSResponse{
			Result: false,
			Errors: err,
		}
	}
	return &SOAJSResponse{
		Result: true,
		Data:   data,
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

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// WriteJSONWithHeaders writes a JSON response with custom headers
func WriteJSONWithHeaders(w http.ResponseWriter, status int, headers map[string]string, data interface{}) error {
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
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
	return WriteJSON(w, status, SOAJSResponse{
		Result: false,
		Errors: NewSOAJSError(code, message),
	})
}

// WriteErrors writes multiple errors in a SOAJS response
func WriteErrors(w http.ResponseWriter, status int, errors []ErrorDetail) error {
	soajsErr := &SOAJSError{
		Codes:   make([]int, len(errors)),
		Details: errors,
	}
	for i, e := range errors {
		soajsErr.Codes[i] = e.Code
	}
	return WriteJSON(w, status, SOAJSResponse{
		Result: false,
		Errors: soajsErr,
	})
}

// ControllerResponse writes a controller-style response
func ControllerResponse(w http.ResponseWriter, status int, data interface{}, headers map[string]string) error {
	// Set custom headers first
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")

	// Check if data has code/msg fields (error case)
	if errData, ok := data.(map[string]interface{}); ok {
		if code, hasCode := errData["code"]; hasCode {
			if msg, hasMsg := errData["msg"]; hasMsg {
				return WriteError(w, status, int(code.(float64)), msg.(string))
			}
		}
	}

	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
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
