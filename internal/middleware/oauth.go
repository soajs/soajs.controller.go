package middleware

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// oauthService is the oauth service and its PIN API, as the Node.js oauth middleware defaults them.
const (
	oauthServiceName = "oauth"
	oauthPinAPI      = "/pin"
)

var bearerRe = regexp.MustCompile(`Bearer\s(\S+)`)

// OAuth authorises the request when mt requires it, as the Node.js oauth middleware does with
// oauth2-server 2.4.1 (authorise): exactly one of the Authorization header and the access_token
// (query string, else header) must carry the token; the token must exist for this environment and
// not be expired. Then the token's deviceId must match the device-id header (156), its
// restrictedTo the request (147), and, on a key's API, the user's PIN state allow the API (145).
//
// oauth2-server errors answer their own code as the HTTP status and SOAJS code, with its texts.
func OAuth(prov provision.Provision, reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				!soajsCtx.Controller.ServiceParams.RequiresOAuth {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams

			// getBearerToken
			headerValues, hasHeader := r.Header["Authorization"]
			queryToken, hasQuery := params.AccessToken, params.HasAccessToken
			if hasHeader && hasQuery {
				WriteError(w, 400, 400, "Only one method may be used to authenticate at a time (Auth header,  GET or POST).")
				return
			}
			if !hasHeader && !hasQuery {
				WriteError(w, 400, 400, "The access token was not found")
				return
			}
			// An empty Authorization header counts as a method but is not parsed: its token is
			// empty, and the model does not find it (401)
			token := queryToken
			if header := strings.Join(headerValues, ", "); header != "" {
				m := bearerRe.FindStringSubmatch(header)
				if m == nil {
					WriteError(w, 400, 400, "Malformed auth header")
					return
				}
				token = m[1]
			}

			// checkToken
			record, err := prov.OAuthModel().GetAccessToken(token)
			if err != nil {
				WriteError(w, 503, 503, err.Error())
				return
			}
			if record == nil {
				WriteError(w, 401, 401, "The access token provided is invalid.")
				return
			}
			if record.Expires.Before(time.Now()) {
				WriteError(w, 401, 401, "The access token provided has expired.")
				return
			}

			if !deviceIDMatch(reg, record, r.Header.Get("device-id")) {
				WriteCode(w, 156)
				return
			}
			if !restrictedToMatch(record, soajsCtx, reg.GetEnvironment(), r.Header.Get("User-Agent")) {
				WriteCode(w, 147)
				return
			}
			params.OAuthToken = record
			params.UserInfo = record.User
			if params.KeyAPI {
				if code := pinCheck(reg, record, soajsCtx, r.Method); code != 0 {
					WriteCode(w, code)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// deviceIDMatch: a token with a deviceId only works with that device-id header, unless the
// custom registry gateway oauth.deviceIdCheck is false.
func deviceIDMatch(reg registry.Registry, record *types.AccessToken, deviceID string) bool {
	if data := reg.Get(); data != nil {
		gateway, _ := data.CustomRegistry["gateway"].(map[string]interface{})
		value, _ := gateway["value"].(map[string]interface{})
		oauth, _ := value["oauth"].(map[string]interface{})
		if check, ok := oauth["deviceIdCheck"].(bool); ok && !check {
			return true
		}
	}
	tokenDevice, _ := record.User["deviceId"].(string)
	if tokenDevice == "" {
		return true
	}
	return tokenDevice == deviceID
}

// restrictedToMatch checks each key of the token's restrictedTo against the request; a restricted
// token on a request without a key never matches.
func restrictedToMatch(record *types.AccessToken, soajsCtx *SOAJSContext, env, userAgent string) bool {
	restrictedTo, _ := record.User["restrictedTo"].(map[string]interface{})
	if restrictedTo == nil {
		return true
	}
	key := soajsCtx.Key
	if key == nil || soajsCtx.Tenant == nil {
		return false
	}
	actual := []struct {
		field, value string
		ignoreCase   bool
	}{
		{"tenant", key.Tenant.ID, false},
		{"product", key.Application.Product, false},
		{"package", key.Application.Package, false},
		{"key", key.ExtKey, false},
		{"env", env, true},
		{"agent", userAgent, false},
	}
	for _, a := range actual {
		allowed, present := restrictedTo[a.field]
		if !present || !truthy(allowed) {
			continue
		}
		if !valueMatch(allowed, a.value, a.ignoreCase) {
			return false
		}
	}
	return true
}

// valueMatch: the request value must be the allowed string, or one of the allowed strings.
func valueMatch(allowed interface{}, actual string, ignoreCase bool) bool {
	if actual == "" {
		return false
	}
	equal := func(s string) bool {
		if ignoreCase {
			return strings.EqualFold(s, actual)
		}
		return s == actual
	}
	switch a := allowed.(type) {
	case string:
		return equal(a)
	case []interface{}:
		for _, one := range a {
			if s, ok := one.(string); ok && s != "" && equal(s) {
				return true
			}
		}
	}
	return false
}

// pinCheck is the PIN part of the Node.js oauthCheck: a urac user whose PIN is locked, or who has
// not logged in with the PIN under a product that requires it, may only call the oauth PIN API,
// the gateway, the custom registry oauth pinWrapper API and pinWhitelist APIs (145).
func pinCheck(reg registry.Registry, record *types.AccessToken, soajsCtx *SOAJSContext, method string) int {
	user := record.User
	if loginMode, _ := user["loginMode"].(string); loginMode != "urac" {
		return 0
	}
	params := soajsCtx.Controller.ServiceParams
	if truthy(user["pinLocked"]) {
		return pinAllowedAPI(reg, params, method)
	}
	product := ""
	if soajsCtx.Key != nil {
		product = soajsCtx.Key.Application.Product
	}
	if product == "" || params.TenantOauth == nil {
		return 0
	}
	if pin, ok := params.TenantOauth.Pin[product]; !ok || !pin.Enabled {
		return 0
	}
	if truthy(user["pinLogin"]) {
		return 0
	}
	return pinAllowedAPI(reg, params, method)
}

func pinAllowedAPI(reg registry.Registry, params *types.ServiceParams, method string) int {
	service := params.URLInfo.ServiceName
	path := params.URLInfo.ServicePath
	if (service == oauthServiceName && path == oauthPinAPI) || service == "controller" {
		return 0
	}
	data := reg.Get()
	if data == nil {
		return 145
	}
	entry, _ := data.CustomRegistry["oauth"].(map[string]interface{})
	value, _ := entry["value"].(map[string]interface{})
	if wrapper, ok := value["pinWrapper"].(map[string]interface{}); ok {
		if wrapper["servicename"] == service && wrapper["apiname"] == path {
			return 0
		}
	}
	whitelist, _ := value["pinWhitelist"].(map[string]interface{})
	byService, _ := whitelist[service].(map[string]interface{})
	rules, _ := byService[strings.ToLower(method)].(map[string]interface{})
	if apis, ok := rules["apis"].([]interface{}); ok {
		for _, a := range apis {
			if a == path {
				return 0
			}
		}
	}
	if patterns, ok := rules["regex"].([]interface{}); ok {
		for _, p := range patterns {
			s, _ := p.(string)
			if re, err := lib.PathToRegexp(s); err == nil && re.MatchString(path) {
				return 0
			}
		}
	}
	return 145
}
