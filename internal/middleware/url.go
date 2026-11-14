package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// URL parses the service, version and path of the request, as the Node.js url middleware does
// (mw/url/index.js, lib/parseURL.js): the root path answers 136, a service the registry does not
// have answers 130. A key sent in the query string is also set as the key header.
func URL(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil {
				next.ServeHTTP(w, r)
				return
			}
			info := parseURL(r.URL.RequestURI())
			soajsCtx.Controller.ServiceParams.URLInfo = info
			if info.ServiceName == "" {
				WriteCode(w, 136)
				return
			}
			if info.ServiceName != "soajs" && reg != nil {
				data := reg.Get()
				if data == nil || data.Services[info.ServiceName] == nil {
					WriteCode(w, 130)
					return
				}
			}
			// Node.js req.query.access_token: the query string's, else the access_token header
			params := soajsCtx.Controller.ServiceParams
			if values, ok := r.URL.Query()["access_token"]; ok {
				params.AccessToken, params.HasAccessToken = values[0], true
			}
			if params.AccessToken == "" {
				if h := r.Header.Get("access_token"); h != "" {
					params.AccessToken, params.HasAccessToken = h, true
				}
			}
			if r.Header.Get("key") == "" {
				if key := r.URL.Query().Get("key"); key != "" {
					r.Header.Set("key", key)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

var (
	versionSegment = regexp.MustCompile(`/v[0-9]+(.[0-9]+)?/`)
	versionSuffix  = regexp.MustCompile(`:[0-9]+(.[0-9]+)?`)
)

// parseURL splits a request URI (path and query) into service, version and path. The version is
// a "/v<N>/" segment right after the service with more after it, or "<service>:<N>".
func parseURL(uri string) *types.URLInfo {
	path := uri
	if i := strings.IndexByte(path, '?'); i != -1 {
		path = path[:i]
	}
	segments := strings.Split(path, "/")
	info := &types.URLInfo{OriginalPath: path}
	if len(segments) < 2 {
		return info
	}
	serviceNV := segments[1]
	info.ServiceName = serviceNV

	if m := versionSegment.FindString(uri); m != "" {
		hit := strings.Trim(m, "/")
		if len(segments) > 3 && segments[2] == hit {
			info.ServiceVersion = strings.TrimPrefix(hit, "v")
			uri = strings.Replace(uri, m, "/", 1)
		}
	}
	if info.ServiceVersion == "" {
		if i := strings.IndexByte(serviceNV, ':'); i != -1 {
			if versionSuffix.MatchString(serviceNV) {
				info.ServiceVersion = serviceNV[i+1:]
			}
			info.ServiceName = serviceNV[:i]
		}
	}
	info.VersionRequested = info.ServiceVersion != ""

	// The service path: what follows "/<service>", without the query; with a query, a trailing
	// slash is dropped too (extractBuildParameters)
	rest := ""
	if len(uri) > len(serviceNV)+1 {
		rest = uri[len(serviceNV)+1:]
	}
	if i := strings.IndexByte(rest, '?'); i != -1 {
		rest = rest[:i]
		if j := strings.LastIndexByte(rest, '/'); j > 0 && j == len(rest)-1 {
			rest = rest[:j]
		}
	}
	info.ServicePath = rest
	return info
}
