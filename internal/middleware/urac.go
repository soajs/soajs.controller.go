package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

var resolverClient = &http.Client{}

// Urac loads the logged-in user on a key's API, after OAuth, as the Node.js mt does with
// uracCheck and aclUrackCheck (mw/mt/utils.js):
//   - a token issued in a sensitive environment and used in another one is roaming: its client
//     tenant and its environment's registry must load, else 170;
//   - the user is read (mw/mt/urac.js); a user that cannot be read, is not active or has no
//     access to the tenant answers 146, on every call, so a paused user is refused at once;
//   - with the key config gateway.validateTokenAccount, a record whose email or phone changed
//     since the token was issued answers 146;
//   - the key's configuration, with the user's (config.keys.<iKey>.config) merged into a copy,
//     becomes the services configuration;
//   - when the user's groups allow packages under the product, their ACL replaces the package
//     ACL for the request.
func Urac(driver *urac.Driver, prov provision.Provision, reg registry.Registry, env string) Middleware {
	regEnvironment := strings.ToLower(env)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			token := params.OAuthToken
			if !params.KeyAPI || params.SkipOAuth || token == nil || soajsCtx.Key == nil || soajsCtx.Tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := soajsCtx.Key
			regData := reg.Get()

			// roaming
			var roaming *urac.Roaming
			if registry.IsSensitiveEnv(strings.ToLower(token.Env)) && token.Env != regEnvironment {
				code := soajsCtx.Tenant.Code
				if soajsCtx.Tenant.ID != token.ClientID {
					c, err := prov.GetTenantCode(token.ClientID)
					if err != nil || c == "" {
						soajsCtx.Log.Error("roaming: tenant not found", zap.String("clientId", token.ClientID), zap.Error(err))
						WriteCode(w, 170)
						return
					}
					code = c
				}
				if data, err := reg.LoadByEnv(token.Env, false); err != nil || data == nil {
					soajsCtx.Log.Error("roaming: registry not found", zap.String("env", token.Env), zap.Error(err))
					WriteCode(w, 170)
					return
				}
				roaming = &urac.Roaming{TID: token.ClientID, ID: token.ClientID, User: token.User, Code: code}
				soajsCtx.Roaming = roaming
			}

			req := &urac.Request{
				Registry:  regData,
				Tenant:    soajsCtx.Tenant,
				Roaming:   roaming,
				Product:   key.Application.Product,
				KeyConfig: key.Config,
				IKey:      key.InternalKey,
				Token:     token,
				Packages:  prov.GetPackageData,
				Logger:    soajsCtx.Log,
			}
			if soajsCtx.Awareness != nil {
				req.Resolve = &urac.ResolveRequest{
					Registry:   regData,
					Tenant:     soajsCtx.Tenant,
					Host:       soajsCtx.Awareness.GetHost,
					HTTPClient: resolverClient,
					Logger:     soajsCtx.Log,
				}
				if resolver := urac.GetResolver(regData, soajsCtx.Tenant, soajsCtx.Log); resolver != nil {
					req.Resolve.InjectObj = urac.InjectObj(soajsCtx.Tenant, key.InternalKey, key.ExtKey,
						key.Application.Product, key.Application.Package, key.Application.AppID, key.Config, resolver.Service)
				}
			}
			user := urac.NewUser(driver, req)
			record, err := user.Init(r.Context())
			if err != nil && roaming == nil {
				soajsCtx.Log.Error("urac", zap.Error(err))
				WriteCode(w, 146)
				return
			}
			if code := validateTokenAccount(key.Config, user.TokenUser(), record); code != 0 {
				WriteCode(w, code)
				return
			}
			userConfig := user.Config()
			if userConfig == nil {
				userConfig = map[string]interface{}{}
			}
			soajsCtx.ServicesConfig = mergeRecursive(deepCopyJSON(key.Config), userConfig)
			soajsCtx.Urac = user

			if acl := user.ACL(); acl != nil {
				params.ACL = uracFinalACL(acl, params, strings.ToLower(r.Method))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validateTokenAccount: with the key config gateway.validateTokenAccount email (or phone), a
// record whose email (or phone) is not the token's answers 146, to force a new login.
func validateTokenAccount(keyConfig, tokenUser, record map[string]interface{}) int {
	gateway, _ := keyConfig["gateway"].(map[string]interface{})
	validate, _ := gateway["validateTokenAccount"].(map[string]interface{})
	if !truthy(validate) || tokenUser == nil || record == nil {
		return 0
	}
	for _, field := range []string{"email", "phone"} {
		if truthy(validate[field]) && truthy(record[field]) && record[field] != tokenUser[field] {
			return 146
		}
	}
	return 0
}

// uracFinalACL is the final ACL from the user's ACL (aclUrackCheck): the requested version, or
// the latest the user's ACL grants, which becomes the request's version. A service or a version
// the user's ACL does not have leaves no final ACL; mt then answers 154.
func uracFinalACL(acl map[string]interface{}, params *types.ServiceParams, method string) *types.FinalACL {
	info := params.URLInfo
	serviceACL, _ := acl[info.ServiceName].(map[string]interface{})
	if serviceACL == nil {
		return nil
	}
	if !info.VersionRequested {
		latest := ""
		for v := range serviceACL {
			if latest == "" || lib.IsLatest(v, latest) {
				latest = v
			}
		}
		if latest != "" {
			info.ServiceVersion = unsanitizeVersion(latest)
		}
	}
	versionACL, _ := serviceACL[sanitizeVersion(info.ServiceVersion)].(map[string]interface{})
	return keyFinalACL(versionACL, method)
}

// mergeRecursive is merge.recursive: objects merge key by key, any other value replaces.
func mergeRecursive(base, extend map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for k, v := range extend {
		b, bok := base[k].(map[string]interface{})
		e, eok := v.(map[string]interface{})
		if bok && eok {
			base[k] = mergeRecursive(b, e)
			continue
		}
		base[k] = deepCopyJSON(v)
	}
	return base
}

func deepCopyJSON[T any](v T) T {
	var out interface{} = v
	switch t := out.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = deepCopyJSON(e)
		}
		out = m
	case []interface{}:
		a := make([]interface{}, len(t))
		for i, e := range t {
			a[i] = deepCopyJSON(e)
		}
		out = a
	}
	return out.(T)
}
