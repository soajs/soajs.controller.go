package registry

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// A registration with mw writes the host and the marketplace item, on a local MongoDB
// (MONGO_URI, default mongodb://127.0.0.1:27017) in a conformance_ database it drops.
func TestRegisterMW(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := drv.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	if err != nil {
		t.Skip("no local MongoDB: " + err.Error())
	}
	defer client.Disconnect(context.Background())
	db := client.Database("conformance_register_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())

	r := NewMongoRegistry(db, nil, zap.NewNop())
	r.env = "dev"
	r.data = &types.RegistryData{Services: map[string]*types.ServiceConfig{}, Daemons: map[string]*types.DaemonConfig{}}
	reg := func(version string, flags map[string]bool, port int) error {
		_, err := r.Register(RegisterOptions{Name: "svc", Group: "G", IP: "10.0.0.1", Type: "service", Version: version, Port: port, MW: true, Flags: flags,
			Maintenance: &types.MaintenanceInfo{Readiness: "/heartbeat", Port: types.PortInfo{Type: "maintenance"}}})
		return err
	}
	if err := reg("1", map[string]bool{"oauth": true}, 4100); err != nil {
		t.Fatal(err)
	}
	if err := reg("1", map[string]bool{"oauth": false}, 4100); err != nil {
		t.Fatal(err)
	}
	if err := reg("2", map[string]bool{}, 4100); err != nil {
		t.Fatal(err)
	}
	var item struct {
		Name     string                   `bson:"name"`
		Versions []map[string]interface{} `bson:"versions"`
	}
	if err := db.Collection(CollMarketplace).FindOne(ctx, bson.M{"name": "svc"}).Decode(&item); err != nil {
		t.Fatal(err)
	}
	if len(item.Versions) != 2 || item.Versions[0]["oauth"] != false || item.Versions[1]["version"] != "2" {
		t.Errorf("marketplace item %+v", item)
	}
	if n, _ := db.Collection(CollHosts).CountDocuments(ctx, bson.M{"name": "svc", "ip": "10.0.0.1"}); n != 2 {
		t.Errorf("hosts: %d", n)
	}
	// another service on the same port
	_, err = r.Register(RegisterOptions{Name: "other", IP: "10.0.0.2", Type: "service", Version: "1", Port: 4100, MW: true})
	if err == nil || !strings.Contains(err.Error(), "is taken by [svc]") {
		t.Errorf("port taken: %v", err)
	}
}
