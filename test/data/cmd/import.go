// Command import loads test data into MongoDB for integration testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/soajs/soajs.controller.go/test/data"
)

func main() {
	uri := flag.String("uri", "mongodb://127.0.0.1:27017", "MongoDB URI")
	db := flag.String("db", "core_provision", "MongoDB database name")
	cleanup := flag.Bool("cleanup", false, "Only cleanup, don't import")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if *cleanup {
		fmt.Printf("Cleaning up database %s...\n", *db)
		if err := data.Cleanup(ctx, *uri, *db); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cleanup complete.")
		return
	}

	fmt.Printf("Importing test data into %s...\n", *db)
	if err := data.Import(ctx, *uri, *db); err != nil {
		fmt.Fprintf(os.Stderr, "Import failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Import complete.")
}
