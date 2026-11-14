package middleware

import (
	"net/http"
)

// KeyACL is where Node.js resolves the key's final ACL (mw/keyACL). The latest data schema has no
// application or extKey ACL, so the package ACL the key middleware loads is the ACL; Node.js's
// version selection and per-method shaping come with the front-of-chain rebuild.
func KeyACL() Middleware {
	return func(next http.Handler) http.Handler {
		return next
	}
}
