package middleware

import (
	"encoding/json"
	"net/http"
)

// ResponseWriter wraps http.ResponseWriter with additional functionality
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code
func (rw *ResponseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

// Write captures that writing has occurred
func (rw *ResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// StatusCode returns the status code
func (rw *ResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// SOAJSResponse represents a standard SOAJS response
type SOAJSResponse struct {
	Result bool        `json:"result"`
	Data   interface{} `json:"data,omitempty"`
	Errors *SOAJSError `json:"errors,omitempty"`
}

// SOAJSError represents a SOAJS error
type SOAJSError struct {
	Codes   []int  `json:"codes"`
	Details []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"details"`
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
		Errors: &SOAJSError{
			Codes: []int{code},
			Details: []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{
				{Code: code, Message: message},
			},
		},
	})
}
