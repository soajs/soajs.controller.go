// Package fixture loads the conformance fixture into a local MongoDB.
//
// The fixture follows the shapes of the live core_provision data (latest schema only). Its values
// are test values: the keys, secrets and tokens in it exist nowhere else.
//
// Every database the fixture writes is named with Prefix, and Load refuses any other prefix or a
// MongoDB that is not on the local machine, so it can never drop real data.
package fixture

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Prefix is the prefix of every fixture database, as in the environment's dbs.config.prefix.
const Prefix = "conformance_"

// files maps each fixture file to the database it loads, without the prefix.
var files = map[string]string{
	"core_provision.json": "core_provision",
	"oauth.json":          "oauth",
	"DBTN_urac.json":      "DBTN_urac",
}

// written are the databases the gateways write during a run (call events): dropped with the
// fixture, so a run starts without them.
var written = []string{"gateway"}

// Load drops the fixture databases and loads them again.
func Load(ctx context.Context, uri string) error {
	if err := checkLocal(uri); err != nil {
		return err
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Disconnect(ctx)

	dir, err := sourceDir()
	if err != nil {
		return err
	}
	for _, name := range written {
		if err := client.Database(Prefix + name).Drop(ctx); err != nil {
			return fmt.Errorf("drop %s: %w", Prefix+name, err)
		}
	}
	for file, name := range files {
		collections, err := readFile(filepath.Join(dir, file))
		if err != nil {
			return err
		}
		db := client.Database(Prefix + name)
		if err := db.Drop(ctx); err != nil {
			return fmt.Errorf("drop %s: %w", db.Name(), err)
		}
		for _, c := range collections {
			docs, ok := c.Value.(bson.A)
			if !ok {
				return fmt.Errorf("%s: %s is not an array", file, c.Key)
			}
			if len(docs) == 0 {
				continue
			}
			if _, err := db.Collection(c.Key).InsertMany(ctx, docs); err != nil {
				return fmt.Errorf("insert %s.%s: %w", db.Name(), c.Key, err)
			}
		}
	}
	return nil
}

// Drop removes the fixture databases.
func Drop(ctx context.Context, uri string) error {
	if err := checkLocal(uri); err != nil {
		return err
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Disconnect(ctx)
	for _, name := range files {
		if err := client.Database(Prefix + name).Drop(ctx); err != nil {
			return fmt.Errorf("drop %s: %w", Prefix+name, err)
		}
	}
	for _, name := range written {
		if err := client.Database(Prefix + name).Drop(ctx); err != nil {
			return fmt.Errorf("drop %s: %w", Prefix+name, err)
		}
	}
	return nil
}

// checkLocal refuses a MongoDB that is not on this machine.
func checkLocal(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid MongoDB uri")
	}
	if u.Scheme != "mongodb" {
		return fmt.Errorf("the fixture only loads into a local mongodb:// server")
	}
	for _, host := range strings.Split(u.Host, ",") {
		h := host
		if i := strings.LastIndex(h, ":"); i != -1 {
			h = h[:i]
		}
		if h != "127.0.0.1" && h != "localhost" {
			return fmt.Errorf("the fixture only loads into a local MongoDB, not %q", h)
		}
	}
	return nil
}

// readFile reads a fixture file in extended JSON, keeping the order of the fields.
func readFile(path string) (bson.D, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var collections bson.D
	if err := bson.UnmarshalExtJSON(raw, false, &collections); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return collections, nil
}

func sourceDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to locate the fixture files")
	}
	return filepath.Dir(file), nil
}
