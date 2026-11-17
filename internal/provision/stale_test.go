package provision

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// the conformance fixture's CONF tenant, key and extKey (test values)
const (
	staleTenantID = "6a0e00000000000000000001"
	staleKey      = "0d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6"
	staleExtKey   = "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609ac1ebf7aecfdc2355849a594cc55d042d9906c2c0190a3b7d5f5935d8053a953ede02160a6f8429f1ba971b8d9d6fe6afe7c59af3462d188c4b1a08e9c07670e"
)

func staleTenant(app string) string {
	return `{"_id":{"$oid":"` + staleTenantID + `"},"code":"CONF","type":"product","oauth":{"disabled":0},
		"applications":[{"appId":{"$oid":"6a0e000000000000000000a2"},"product":"CONF","package":"CONF_DEFAULT","_TTL":1` + app + `,
		"keys":[{"key":"` + staleKey + `","config":{"dev":{}},"extKeys":[{"extKey":"` + staleExtKey + `","env":"DEV"}]}]}]}`
}

// A cached key the tenant no longer gives, or whose tenant cannot be read, is refused as Node.js
// refuses a missing key (153), never served from the old entry.
func TestCachedKeyForgotten(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := drv.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	if err != nil {
		t.Skip("no local MongoDB: " + err.Error())
	}
	defer client.Disconnect(context.Background())
	db := client.Database("conformance_provision_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())
	tenants := db.Collection("tenants")
	put := func(doc string) {
		t.Helper()
		var d bson.D
		if err := bson.UnmarshalExtJSON([]byte(doc), false, &d); err != nil {
			t.Fatal(err)
		}
		if _, err := tenants.DeleteMany(ctx, bson.M{}); err != nil {
			t.Fatal(err)
		}
		if _, err := tenants.InsertOne(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	p := newTestProvision()
	p.db = db
	p.SetKeyConfig("aes256", "conformance fixture key")
	resolve := func() (*types.Key, error) {
		time.Sleep(5 * time.Millisecond) // past the application's _TTL of 1 ms: read again
		return p.GetExternalKeyData(staleExtKey)
	}
	refused := func(name string) {
		t.Helper()
		k, err := resolve()
		var ce *CodeError
		if k != nil || err == nil || !errorsAs(err, &ce) || ce.Code != 153 {
			t.Errorf("%s: refused with 153, got %+v %v", name, k, err)
		}
	}

	put(staleTenant(""))
	if k, err := resolve(); err != nil || k == nil {
		t.Fatalf("the key resolves: %v", err)
	}
	put(staleTenant(`,"acl":{"svc":{}}`))
	refused("its application is left out (an application ACL)")
	put(staleTenant(""))
	if k, _ := resolve(); k == nil {
		t.Fatal("the fixed tenant gives the key again")
	}
	put(`{"_id":{"$oid":"` + staleTenantID + `"},"code":"conf","type":"product","applications":[]}`)
	refused("its tenant cannot be read")
	put(staleTenant(""))
	if k, _ := resolve(); k == nil {
		t.Fatal("the fixed tenant gives the key again")
	}
	if _, err := tenants.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatal(err)
	}
	refused("no tenant gives it")
}

// forgetKeys never touches a key another tenant gives now.
func TestForgetKeysKeepsMovedKey(t *testing.T) {
	p := newTestProvision()
	p.keys["k"] = &keyEntry{key: "k", tenant: types.KeyTenant{ID: "B"}}
	p.tenantKeys["A"] = map[string]bool{"k": true}
	p.forgetTenant("A")
	if p.keys["k"] == nil {
		t.Error("the key now belongs to tenant B")
	}
}

func errorsAs(err error, target **CodeError) bool {
	ce, ok := err.(*CodeError)
	if ok {
		*target = ce
	}
	return ok
}
