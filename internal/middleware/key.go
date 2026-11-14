package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.uber.org/zap"
)

// Key resolves the request's key, as the Node.js key middleware does (mw/key/index.js): an
// external key that does not resolve answers 148, one of another environment 144. The key's
// package, after the network override, is loaded for the next middlewares, and the key's
// configuration becomes the request's servicesConfig.
func Key(prov provision.Provision, reg registry.Registry, env string) Middleware {
	env = strings.ToLower(env)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			key := r.Header.Get("key")
			if key == "" {
				key = r.URL.Query().Get("key")
			}
			if key == "" || soajsCtx == nil {
				next.ServeHTTP(w, r)
				return
			}

			keyObj, err := prov.GetExternalKeyData(key)
			if err != nil {
				soajsCtx.Log.Error("unable to resolve the key", zap.Error(err))
			}
			if keyObj == nil {
				WriteCode(w, 148)
				return
			}
			soajsCtx.Key = keyObj
			if keyObj.Application.Package == "" {
				next.ServeHTTP(w, r)
				return
			}
			if keyObj.Env != "" && strings.ToLower(keyObj.Env) != env {
				WriteCode(w, 144)
				return
			}

			// Network package override: the custom registry gateway lastSeen.network picks the
			// key's networkPackages entry for the application's product
			if reg != nil {
				if network := lastSeenNetwork(reg); network != "" {
					if pkg := networkPackage(keyObj.Config, network, keyObj.Application.Product); pkg != "" {
						keyObj.Application.Package = pkg
					}
				}
			}

			pack, err := prov.GetPackageData(keyObj.Application.Package)
			if err != nil {
				soajsCtx.Log.Error("unable to load the key's package", zap.String("package", keyObj.Application.Package), zap.Error(err))
				next.ServeHTTP(w, r)
				return
			}
			if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
				soajsCtx.Controller.ServiceParams.Package = pack
			}
			soajsCtx.ServicesConfig = keyObj.Config
			next.ServeHTTP(w, r)
		})
	}
}

// lastSeenNetwork is the custom registry gateway value.lastSeen.network.
func lastSeenNetwork(reg registry.Registry) string {
	data := reg.Get()
	if data == nil {
		return ""
	}
	gateway, _ := data.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	lastSeen, _ := value["lastSeen"].(map[string]interface{})
	network, _ := lastSeen["network"].(string)
	return network
}

// networkPackage is the key config's gateway.networkPackages.<network>.<product>.default.
func networkPackage(config map[string]interface{}, network, product string) string {
	gateway, _ := config["gateway"].(map[string]interface{})
	packages, _ := gateway["networkPackages"].(map[string]interface{})
	byNetwork, _ := packages[network].(map[string]interface{})
	byProduct, _ := byNetwork[product].(map[string]interface{})
	pkg, _ := byProduct["default"].(string)
	return pkg
}
