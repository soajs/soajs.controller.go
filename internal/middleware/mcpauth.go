package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// The MCP authorization flow (OAuth 2.1, RFC 9728, RFC 8414) of /soajs/mcp (mw/soajsRoutes/mcp/
// auth.js), on when the custom registry gateway mcp.auth is set: the gateway is the protected
// resource, answers a call without a valid token 401 with the address of its metadata, and serves
// the metadata, which names the authorization server endpoints of the configuration: by the
// product of the request's key (auth.byProduct), else by the request's host (auth.byHost), else
// auth.default. Addresses use the request's host and auth.scheme, else X-Forwarded-Proto, else
// the connection's scheme.
const (
	mcpPath    = "/soajs/mcp"
	issuerPath = mcpPath + "/auth/"
	prmPath    = "/.well-known/oauth-protected-resource"
)

var asPaths = []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"}

// mcpAuthConfig is mcp.auth when mcp is enabled.
func mcpAuthConfig(regData *types.RegistryData) map[string]interface{} {
	mcp := mcpEntry(regData)
	if mcp == nil || mcp["enabled"] != true {
		return nil
	}
	auth, _ := mcp["auth"].(map[string]interface{})
	return auth
}

// usableEntry names the authorization and token endpoints.
func usableEntry(v interface{}) map[string]interface{} {
	e, _ := v.(map[string]interface{})
	if str(e["authorization_endpoint"]) == "" || str(e["token_endpoint"]) == "" {
		return nil
	}
	return e
}

func ownEntry(v interface{}, name string) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	if m == nil || name == "" {
		return nil
	}
	return usableEntry(m[name])
}

func firstValue(h string) string {
	if h == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(h, ",")[0])
}

func requestHost(r *http.Request) string {
	h := firstValue(r.Header.Get("X-Forwarded-Host"))
	if h == "" {
		h = r.Host
	}
	return strings.ToLower(h)
}

func requestOrigin(r *http.Request, auth map[string]interface{}) string {
	proto := str(auth["scheme"])
	if proto != "https" && proto != "http" {
		proto = firstValue(r.Header.Get("X-Forwarded-Proto"))
	}
	if proto == "" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	return proto + "://" + requestHost(r)
}

type authMatch struct {
	id    string
	entry map[string]interface{}
}

// resolveEntry is the entry of a request: by the product of the key, by the host, the default.
func resolveEntry(r *http.Request, prov provision.Provision, auth map[string]interface{}, key string) *authMatch {
	if key != "" && auth["byProduct"] != nil {
		product := ""
		if k, _ := prov.GetExternalKeyData(key); k != nil {
			product = k.Application.Product
		}
		if e := ownEntry(auth["byProduct"], product); e != nil {
			return &authMatch{"product/" + product, e}
		}
	}
	if e := ownEntry(auth["byHost"], requestHost(r)); e != nil {
		return &authMatch{"host", e}
	}
	if e := usableEntry(auth["default"]); e != nil {
		return &authMatch{"default", e}
	}
	return nil
}

func entryByID(r *http.Request, auth map[string]interface{}, id string) map[string]interface{} {
	switch {
	case id == "host":
		return ownEntry(auth["byHost"], requestHost(r))
	case id == "default":
		return usableEntry(auth["default"])
	case strings.HasPrefix(id, "product/"):
		return ownEntry(auth["byProduct"], strings.TrimPrefix(id, "product/"))
	}
	return nil
}

func authJSON(w http.ResponseWriter, status int, body *omap, headers map[string]string) {
	text := jsStringify(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(text)))
	w.Header().Set("Cache-Control", "no-store")
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}

// WellKnown serves the metadata of the MCP authorization flow, before the url middleware:
//   - GET /.well-known/oauth-protected-resource[/soajs/mcp][?key=]: the resource and its
//     authorization server, the issuer of the entry the key or the request chooses;
//   - GET /.well-known/{oauth-authorization-server|openid-configuration}/soajs/mcp/auth/<id>: the
//     endpoints of that entry.
//
// Any other request goes on.
func WellKnown(reg registry.Registry, prov provision.Provision) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uri := r.RequestURI
			path := uri
			if i := strings.IndexByte(uri, '?'); i != -1 {
				path = uri[:i]
			}
			if !strings.HasPrefix(path, "/.well-known/") {
				next.ServeHTTP(w, r)
				return
			}
			auth := mcpAuthConfig(reg.Get())
			if auth == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
				next.ServeHTTP(w, r)
				return
			}
			if path == prmPath || path == prmPath+mcpPath {
				queryKey := ""
				if i := strings.IndexByte(uri, '?'); i != -1 {
					queryKey = firstQueryValue(uri[i:], "key")
				}
				match := resolveEntry(r, prov, auth, queryKey)
				if match == nil {
					next.ServeHTTP(w, r)
					return
				}
				resource := requestOrigin(r, auth) + mcpPath
				if queryKey != "" {
					resource += "?key=" + escapeComponent(queryKey)
				}
				authJSON(w, http.StatusOK, newOmap().
					set("resource", resource).
					set("authorization_servers", []interface{}{requestOrigin(r, auth) + issuerPath + match.id}).
					set("bearer_methods_supported", []interface{}{"header"}), nil)
				return
			}
			for _, p := range asPaths {
				prefix := p + issuerPath
				if !strings.HasPrefix(path, prefix) {
					continue
				}
				id, err := decodeURIComponent(path[len(prefix):])
				if err != nil {
					break
				}
				entry := entryByID(r, auth, id)
				if entry == nil {
					break
				}
				metadata := newOmap().
					set("issuer", requestOrigin(r, auth)+issuerPath+id).
					set("authorization_endpoint", entry["authorization_endpoint"]).
					set("token_endpoint", entry["token_endpoint"]).
					set("response_types_supported", []interface{}{"code"}).
					set("grant_types_supported", []interface{}{"authorization_code"}).
					set("code_challenge_methods_supported", []interface{}{"S256"}).
					set("token_endpoint_auth_methods_supported", []interface{}{"none"})
				if re := str(entry["registration_endpoint"]); re != "" {
					metadata.set("registration_endpoint", re)
				}
				authJSON(w, http.StatusOK, metadata, nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// firstQueryValue is URLSearchParams.get: the first value of a parameter.
func firstQueryValue(search, name string) string {
	for _, pair := range strings.Split(strings.TrimPrefix(search, "?"), "&") {
		k, v, _ := strings.Cut(pair, "=")
		if unescapeQuery(k) == name {
			return utf8Body([]byte(unescapeQuery(v)))
		}
	}
	return ""
}

// tokenKey is the key a token is restricted to, when it is exactly one (user.restrictedTo.key).
func tokenKey(record *types.AccessToken) string {
	restrictedTo, _ := record.User["restrictedTo"].(map[string]interface{})
	switch k := restrictedTo["key"].(type) {
	case string:
		return k
	case []interface{}:
		if len(k) == 1 {
			s, _ := k[0].(string)
			return s
		}
	}
	return ""
}

// MCPAuthorize runs on /soajs/mcp, after the url middleware and before the key middleware, when
// mcp is enabled:
//   - with mcp.auth, a call without a token, or with one that is unknown or expired, answers 401
//     with WWW-Authenticate pointing at the metadata, when an entry applies;
//   - a token restricted to one key brings it: without a key header it becomes the key; with a
//     key of the same product it replaces it; a key of another product is left, and OAuth then
//     refuses the token (147).
func MCPAuthorize(reg registry.Registry, prov provision.Provision) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			segments := strings.Split(soajsCtx.Controller.ServiceParams.URLInfo.OriginalPath, "/")
			if len(segments) < 3 || segments[1] != "soajs" || segments[2] != "mcp" {
				next.ServeHTTP(w, r)
				return
			}
			regData := reg.Get()
			mcp := mcpEntry(regData)
			if mcp == nil || mcp["enabled"] != true {
				next.ServeHTTP(w, r)
				return
			}
			auth := mcpAuthConfig(regData)
			queryKey := parseQueryString(r.URL.RawQuery).get("key")
			askToAuthorize := func(invalid bool) {
				if auth == nil {
					next.ServeHTTP(w, r)
					return
				}
				if resolveEntry(r, prov, auth, r.Header.Get("key")) == nil {
					next.ServeHTTP(w, r)
					return
				}
				metadata := requestOrigin(r, auth) + prmPath + mcpPath
				if queryKey != "" {
					metadata += "?key=" + escapeComponent(queryKey)
				}
				value := `Bearer resource_metadata="` + metadata + `"`
				body := newOmap().set("error", "unauthorized").set("error_description", "Authorization required.")
				if invalid {
					value += `, error="invalid_token"`
					body = newOmap().set("error", "invalid_token").set("error_description", "The access token is invalid or has expired.")
				}
				authJSON(w, http.StatusUnauthorized, body, map[string]string{
					"WWW-Authenticate":              value,
					"Access-Control-Expose-Headers": "WWW-Authenticate",
				})
			}
			token := soajsCtx.Controller.ServiceParams.AccessToken
			if token == "" {
				if m := mcpBearer.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
					token = m[1]
				}
			}
			if token == "" {
				askToAuthorize(false)
				return
			}
			record, err := prov.OAuthModel().GetAccessToken(token)
			if err != nil {
				soajsCtx.Log.Error("MCP: unable to read the access token", zap.Error(err))
				next.ServeHTTP(w, r)
				return
			}
			if record == nil || record.Expires.Before(time.Now()) {
				askToAuthorize(true)
				return
			}
			restricted := tokenKey(record)
			given := r.Header.Get("key")
			switch {
			case restricted == "" || given == restricted:
			case given == "":
				r.Header.Set("key", restricted)
			default:
				g, _ := prov.GetExternalKeyData(given)
				own, _ := prov.GetExternalKeyData(restricted)
				if g != nil && own != nil && g.Application.Product != "" && g.Application.Product == own.Application.Product {
					r.Header.Set("key", restricted)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
