package dataschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureDoc is a document of the conformance fixture, as extended JSON values.
func fixtureDoc(t *testing.T, coll string, i int) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(DefaultDir(), "..", "..", "test", "conformance", "fixture", "core_provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string][]map[string]interface{}
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	return all[coll][i]
}

const serviceBase = `{"_id":{"$oid":"6a0e000000000000000000c1"},"name":"svc","type":"service",
	"configuration":{"group":"g","port":4100,"requestTimeout":30,"requestTimeoutRenewal":5},"versions":[%s]}`

func version(v, extra string) string {
	return `{"version":"` + v + `","interConnect":[],"maintenance":{"readiness":"/heartbeat","port":{"type":"maintenance"}},"apis":[],
		"extKeyRequired":true,"oauth":true,"urac":false,"urac_Profile":false,"urac_ACL":false,"urac_Config":false,
		"urac_GroupConfig":false,"tenant_Profile":false,"provision_ACL":false` + extra + `}`
}

func TestMarketplaceInterConnectNull(t *testing.T) {
	findings, ok, out := readDoc(t, "marketplace", Marketplace, fmtDoc(serviceBase, version("1", `,"interConnect":null`)[:0]+
		`{"version":"1","interConnect":null,"maintenance":null,"extKeyRequired":"yes","oauth":0}`))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	v := dig(out, "versions", 0)
	if dig(v, "interConnect") != nil || dig(v, "maintenance") != nil || dig(v, "extKeyRequired") != true || dig(v, "oauth") != false {
		t.Errorf("Node.js readings: %s", asJSON(v))
	}
	if !has(findings, "/versions/0/interConnect", ReadAsNode) {
		t.Errorf("findings: %+v", findings)
	}
}

func TestMarketplaceVersionUnits(t *testing.T) {
	// an older version that cannot be read is left out
	findings, ok, out := readDoc(t, "marketplace", Marketplace, fmtDoc(serviceBase,
		version("1", `,"apis":"none"`)+","+version("2", "")))
	if !ok || len(out["versions"].([]interface{})) != 1 || dig(out, "versions", 0, "version") != "2" {
		t.Errorf("older version: %v %+v %s", ok, findings, asJSON(out))
	}
	// the latest version cannot be read: the service is skipped (130), not routed to an older one
	findings, ok, _ = readDoc(t, "marketplace", Marketplace, fmtDoc(serviceBase,
		version("1", "")+","+version("2", `,"apis":"none"`)))
	if ok || findings[len(findings)-1].Action != SkippedDocument {
		t.Errorf("latest version: %v %+v", ok, findings)
	}
	// so is a version whose number cannot be read
	if _, ok, _ := readDoc(t, "marketplace", Marketplace, fmtDoc(serviceBase, version("1", "")+","+version("x", ""))); ok {
		t.Error("a version without a number may be the latest")
	}
	// and a service without a port
	if _, ok, _ := readDoc(t, "marketplace", Marketplace, `{"_id":{"$oid":"6a0e000000000000000000c1"},"name":"svc","type":"service",
		"configuration":{"group":"g","port":"x"},"versions":[`+version("1", "")+`]}`); ok {
		t.Error("no port, no service")
	}
	// Node.js defaults for the configuration
	findings, ok, out = readDoc(t, "marketplace", Marketplace, `{"_id":{"$oid":"6a0e000000000000000000c1"},"name":"svc","type":"service",
		"configuration":{"group":"","port":4100,"requestTimeout":0,"requestTimeoutRenewal":"5"},"versions":[`+version("1", "")+`]}`)
	if !ok || dig(out, "configuration", "group") != "service" || dig(out, "configuration", "requestTimeout") != 30.0 ||
		dig(out, "configuration", "requestTimeoutRenewal") != nil {
		t.Errorf("configuration: %v %+v %s", ok, findings, asJSON(out))
	}
}

const gatewayBase = `{"_id":{"$oid":"6a0e000000000000000000d1"},"name":"gateway","created":"DEV","plugged":true,"shared":false,"sharedEnvs":{},"value":{%s}}`

func TestCustomRegistryGatewayFeatures(t *testing.T) {
	findings, ok, out := readDoc(t, "custom_registry", CustomRegistry, fmtDoc(gatewayBase,
		`"events":{"enabled":true,"sink":"kafka"},"mcp":{"enabled":true},"maintenanceMode":{"on":"yes","status":"x","message":""},
		"traffic":{"model":"memory","ip2ban":["1.2.3.4",5],"throttling":{"good":{"publicAPIStrategy":"a"},"bad":{"apis":"x"}}},
		"oauth":{"deviceIdCheck":"no"},"mt":{"whitelist":{"ips":"10.0.0.0/8","acl":true}},"future":{"x":1}`))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	v := out["value"].(map[string]interface{})
	if v["events"] != nil || v["mcp"] == nil || v["future"] != nil {
		t.Errorf("one feature left out, the others kept: %s", asJSON(v))
	}
	if dig(v, "maintenanceMode", "on") != true || dig(v, "maintenanceMode", "status") != nil || dig(v, "maintenanceMode", "message") != nil {
		t.Errorf("maintenance mode stays on: %s", asJSON(v["maintenanceMode"]))
	}
	if asJSON(dig(v, "traffic", "ip2ban")) != `["1.2.3.4"]` || dig(v, "traffic", "throttling", "bad") != nil || dig(v, "traffic", "throttling", "good") == nil {
		t.Errorf("traffic: %s", asJSON(v["traffic"]))
	}
	if dig(v, "oauth", "deviceIdCheck") != true || dig(v, "mt", "whitelist") != nil {
		t.Errorf("deviceIdCheck on unless false; a whitelist without an ips list is none: %s", asJSON(v))
	}
	if !has(findings, "/value/future", IgnoredField) {
		t.Errorf("findings: %+v", findings)
	}
	// the urac entry: masterDB is needed, an access resolver is left out alone
	_, ok, out = readDoc(t, "custom_registry", CustomRegistry, `{"_id":{"$oid":"6a0e000000000000000000d2"},"name":"urac","created":"DEV",
		"value":{"masterDB":"m","tenantAccessResolvers":{"a":{"service":"s","api":"/a"},"b":{"service":"s"}}}}`)
	if !ok || dig(out, "value", "tenantAccessResolvers", "a") == nil || dig(out, "value", "tenantAccessResolvers", "b") != nil {
		t.Errorf("urac resolvers: %v %s", ok, asJSON(out))
	}
	if _, ok, _ := readDoc(t, "custom_registry", CustomRegistry, `{"_id":{"$oid":"6a0e000000000000000000d2"},"name":"urac","created":"DEV","value":{"masterDB":5}}`); ok {
		t.Error("no masterDB, no urac entry")
	}
}

func TestEnvironmentField(t *testing.T) {
	env := fixtureDoc(t, "environment", 0)
	env["description"] = 5.0
	services := env["services"].(map[string]interface{})
	services["controller"].(map[string]interface{})["authorization"] = 1.0
	cors := services["config"].(map[string]interface{})["cors"].(map[string]interface{})
	cors["credentials"] = "true"
	cors["enabled"] = "on"
	findings, ok, out := readDoc(t, "environment", Environment, asJSON(env))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	if out["description"] != nil || dig(out, "services", "config", "cors", "credentials") != true ||
		dig(out, "services", "config", "cors", "enabled") != true || dig(out, "services", "controller", "authorization") != true {
		t.Errorf("environment: %s", asJSON(out))
	}
	if !has(findings, "/description", DroppedPiece) || !has(findings, "/services/config/cors/credentials", ReadAsNode) {
		t.Errorf("findings: %+v", findings)
	}
	delete(services, "config")
	if findings, ok, _ := readDoc(t, "environment", Environment, asJSON(env)); ok || findings[len(findings)-1].Action != SkippedDocument {
		t.Errorf("an environment without a field the gateway needs cannot be read: %+v", findings)
	}
}

func TestUsersAndGroupsUnits(t *testing.T) {
	user := `{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","status":"active","tenant":{"id":"6a0e00000000000000000001","code":"CONF"},
		"groups":["g"],"profile":{},"config":{"allowedTenants":[{"tenant":{"id":"6a0e00000000000000000002"}},{"tenant":{"id":"6a0e00000000000000000002","code":"DBTN"}}]}}`
	findings, ok, out := readDoc(t, "users", Users, user)
	if !ok || len(dig(out, "config", "allowedTenants").([]interface{})) != 1 {
		t.Errorf("one invitation left out: %v %+v", ok, findings)
	}
	if _, ok, _ := readDoc(t, "users", Users, `{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","status":"active",
		"tenant":{"id":"6a0e00000000000000000001","code":"CONF"},"groups":"g"}`); ok {
		t.Error("without its groups the user cannot be read")
	}
	group := `{"_id":{"$oid":"6a0e000000000000000000c7"},"code":"g","tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"config":{%s}}`
	if _, ok, _ := readDoc(t, "groups", Groups, fmtDoc(group, `"allowedPackages":{"P":"P_D"}`)); ok {
		t.Error("a group whose packages cannot be read is not read in part")
	}
	if _, ok, out := readDoc(t, "groups", Groups, fmtDoc(group, `"allowedPackages":{"P":["P_D"]},"allowedEnvironments":5`)); !ok || dig(out, "config", "allowedEnvironments") != nil {
		t.Error("allowedEnvironments is left out alone")
	}
	// a token never loses a field it needs, and an unknown restriction refuses it
	if _, ok, _ := readDoc(t, "oauth_token", OAuthTokens, `{"_id":{"$oid":"6a0e000000000000000000f9"},"token":"c0f0000000000000000000000000000000000001","type":"accessToken","clientId":"c","env":"dev","expires":{"$date":"2099-01-01T00:00:00Z"},
		"user":{"_id":{"$oid":"6a0e000000000000000000b7"},"id":"6a0e000000000000000000b7","username":"u","loginMode":"urac","deviceId":null,
		"tenant":{"id":"6a0e00000000000000000001","code":"CONF"},"restrictedTo":{"tenant":"x","service":"s"}}}`); ok {
		t.Error("an unknown restriction refuses the token")
	}
	if _, ok, _ := readDoc(t, "oauth_token", OAuthTokens,
		`{"_id":{"$oid":"6a0e000000000000000000f9"},"token":"c0f0000000000000000000000000000000000001","type":"accessToken","clientId":"c","env":"dev","expires":{"$date":"2099-01-01T00:00:00Z"},"extra":1,
		"user":{"_id":{"$oid":"6a0e000000000000000000b7"},"id":"6a0e000000000000000000b7","username":"u","loginMode":"urac","deviceId":null,
		"tenant":{"id":"6a0e00000000000000000001","code":"CONF"}}}`); !ok {
		t.Error("an unknown field elsewhere in a token is ignored")
	}
	if _, ok, _ := readDoc(t, "oauth_token", OAuthTokens,
		`{"_id":{"$oid":"6a0e000000000000000000f9"},"token":"c0f0000000000000000000000000000000000001","type":"accessToken","clientId":"c","env":"dev","expires":{"$date":"2099-01-01T00:00:00Z"},
		"user":{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","loginMode":"urac","deviceId":null,"tenant":{"id":"6a0e00000000000000000001","code":"CONF"}}}`); ok {
		t.Error("a token without its user's id is refused")
	}
}
