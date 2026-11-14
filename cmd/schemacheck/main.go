// Command schemacheck checks a SOAJS database against the gateway data schema (docs/data-schema).
//
// It only reads. It prints, per collection, how many documents match the schema and the _id of
// each one that does not; -explain adds where each document breaks the schema (field and schema
// locations, never values). The connection string is read from SOAJS_MONGO_URI and never printed.
//
//	SOAJS_MONGO_URI=... schemacheck -provision core_provision -oauth prod_oauth -urac DBTN_urac
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
)

func main() {
	schemas := flag.String("schemas", dataschema.DefaultDir(), "directory of *.schema.json")
	provision := flag.String("provision", "core_provision", "provision database")
	oauth := flag.String("oauth", "", "comma-separated oauth databases (the provision database is always checked)")
	urac := flag.String("urac", "", "comma-separated tenant urac databases")
	gateway := flag.String("gateway", "", "comma-separated gateway databases")
	explain := flag.Bool("explain", false, "print where each document breaks the schema")
	flag.Parse()

	uri := os.Getenv("SOAJS_MONGO_URI")
	if uri == "" {
		fail("set SOAJS_MONGO_URI")
	}
	collections, err := dataschema.Load(*schemas)
	if err != nil {
		fail(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// Read only: secondary preferred, and only find is ever called.
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetReadPreference(readpref.SecondaryPreferred()))
	if err != nil {
		fail("unable to connect")
	}
	defer client.Disconnect(ctx)

	databases := map[string][]string{
		dataschema.DBProvision: {*provision},
		dataschema.DBOAuth:     append([]string{*provision}, list(*oauth)...),
		dataschema.DBUrac:      list(*urac),
		dataschema.DBGateway:   list(*gateway),
	}

	fmt.Printf("%-24s %-20s %8s %8s %8s\n", "collection", "database", "total", "valid", "invalid")
	exit := 0
	for _, c := range collections {
		for _, db := range databases[c.Database] {
			total, valid, invalid, err := check(ctx, client.Database(db).Collection(c.Name), c, *explain)
			if err != nil {
				fmt.Printf("%-24s %-20s error: %v\n", c.Name, db, err)
				exit = 1
				continue
			}
			fmt.Printf("%-24s %-20s %8d %8d %8d\n", c.Name, db, total, valid, len(invalid))
			for _, id := range invalid {
				fmt.Printf("    %s\n", id)
			}
			if len(invalid) > 0 {
				exit = 1
			}
		}
	}
	os.Exit(exit)
}

func check(ctx context.Context, coll *mongo.Collection, c *dataschema.Collection, explain bool) (int, int, []string, error) {
	cursor, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return 0, 0, nil, err
	}
	defer cursor.Close(ctx)
	total, valid := 0, 0
	var invalid []string
	for cursor.Next(ctx) {
		total++
		problems, err := c.ValidateBSON(cursor.Current)
		if err != nil {
			return 0, 0, nil, err
		}
		if len(problems) == 0 {
			valid++
			continue
		}
		line := id(cursor.Current)
		if explain {
			for _, p := range problems {
				line += "\n        " + p.Instance + "  (" + p.Rule + ")"
			}
		}
		invalid = append(invalid, line)
	}
	return total, valid, invalid, cursor.Err()
}

// id prints a document's _id: the hex of an ObjectId, else its type only.
func id(doc bson.Raw) string {
	v, err := doc.LookupErr("_id")
	if err != nil {
		return "(no _id)"
	}
	if oid, ok := v.ObjectIDOK(); ok {
		return oid.Hex()
	}
	return "(_id of type " + v.Type.String() + ")"
}

func list(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "schemacheck:", msg)
	os.Exit(2)
}
