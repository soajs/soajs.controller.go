package server

import (
	"testing"
)

// Environment values a Node.js reading replaced are dumped as stored, as Node.js dumps them.
func TestPatchRaw(t *testing.T) {
	dump := map[string]interface{}{"awareness": map[string]interface{}{"autoRegisterService": true}, "ports": map[string]interface{}{}}
	patchRaw(dump, map[string]interface{}{
		"/services/config/awareness/autoRegisterService": "yes",
		"/services/config/ports/missing":                 1,
		"/services/controller/authorization":             1,
	}, "/services/config/")
	if dump["awareness"].(map[string]interface{})["autoRegisterService"] != "yes" {
		t.Errorf("raw value: %v", dump)
	}
	if _, added := dump["ports"].(map[string]interface{})["missing"]; added {
		t.Error("only fields the dump has are patched")
	}
	controller := map[string]interface{}{"authorization": true}
	patchRaw(controller, map[string]interface{}{"/services/controller/authorization": 1}, "/services/controller/")
	if controller["authorization"] != 1 {
		t.Errorf("controller: %v", controller)
	}
}
