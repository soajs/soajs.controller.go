package middleware

import "net/http"

// Middleware is the standard middleware function signature
type Middleware func(http.Handler) http.Handler

// Chain creates a new middleware chain
func Chain(middlewares ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
