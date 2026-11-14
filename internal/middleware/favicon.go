package middleware

import (
	"net/http"
)

// Favicon answers /favicon.ico with an empty image/x-icon, as the Node.js gateway does.
func Favicon() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RequestURI() == "/favicon.ico" {
				w.Header().Set("Content-Type", "image/x-icon")
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
