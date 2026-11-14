package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/lib"
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
			uri := r.RequestURI
			if uri == "" {
				uri = r.URL.RequestURI()
			}
			info, err := parseURL(uri)
			if err != nil {
				// Node.js new URL() throws, and the error answers 500 with its code
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"result":false,"errors":{"codes":["ERR_INVALID_URL"],"details":[{"code":"ERR_INVALID_URL","message":"Invalid URL"}]}}`))
				return
			}
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

// parseURL splits a request target into service, version and path, as lib/parseURL.js does on
// the URL Node.js parses (WHATWG new URL(target, "http://localhost")): the service is the first
// segment of the parsed path; the version is a "/v<N>/" segment of the target right after the
// service with more after it, the target then parsed again without it, or "<service>:<N>". Every
// path and the URL the service is called with come from the parsed URL, so the API checked is the
// API called.
func parseURL(target string) (*types.URLInfo, error) {
	pathname, search, err := lib.RequestURL(target)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(pathname, "/")
	info := &types.URLInfo{OriginalPath: pathname}
	if len(segments) < 2 {
		return info, nil
	}
	serviceNV := segments[1]
	info.ServiceName = serviceNV

	if m := versionSegment.FindString(target); m != "" {
		hit := strings.Trim(m, "/")
		if len(segments) > 3 && segments[2] == hit {
			info.ServiceVersion = strings.TrimPrefix(hit, "v")
			if pathname, search, err = lib.RequestURL(strings.Replace(target, m, "/", 1)); err != nil {
				return nil, err
			}
			info.OriginalPath = pathname
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

	// The service's URL: what follows "/<service>" in the parsed path and query; its path drops
	// the query and, with a query, a trailing slash (extractBuildParameters)
	full := pathname + search
	rest := ""
	if len(full) > len(serviceNV)+1 {
		rest = full[len(serviceNV)+1:]
	}
	info.URL = rest
	info.FullPath = pathname
	if i := strings.IndexByte(rest, '?'); i != -1 {
		rest = rest[:i]
		if j := strings.LastIndexByte(rest, '/'); j > 0 && j == len(rest)-1 {
			rest = rest[:j]
		}
	}
	info.ServicePath = rest
	return info, nil
}
