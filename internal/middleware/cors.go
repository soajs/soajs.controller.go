package middleware

import (
	"net/http"
	"strconv"

	"github.com/soajs/soajs.controller.go/internal/registry"
)

// CORS answers as the Node.js cors middleware does (mw/cors/index.js), from the registry's
// serviceConfig.cors when it is enabled: a preflight (OPTIONS) gets every header and 204; any other
// request gets the origin, credentials and exposed headers and goes on.
func CORS(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := reg.Get()
			if data == nil || data.ServiceConfig == nil || !data.ServiceConfig.CORS.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			cors := data.ServiceConfig.CORS
			origin := orDefault(cors.Origin, "*")
			// Node.js writes credentials || 'true': false is written as true too
			credentials := "true"
			methods := orDefault(cors.Methods, "GET,HEAD,PUT,PATCH,POST,DELETE")
			headers := orDefault(cors.Headers, "__env,key,soajsauth,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type")
			maxAge := cors.MaxAge
			if maxAge == 0 {
				maxAge = 1728000
			}

			h := w.Header()
			if !cors.OriginRefused {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			h.Set("Access-Control-Allow-Credentials", credentials)
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", headers)
				h.Set("Access-Control-Max-Age", strconv.Itoa(maxAge))
				h.Set("Access-Control-Expose-Headers", headers)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.Set("Access-Control-Expose-Headers", headers)
			next.ServeHTTP(w, r)
		})
	}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
