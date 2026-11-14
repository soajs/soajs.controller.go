// Command fixture loads the conformance fixture into a local MongoDB, or drops it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/soajs/soajs.controller.go/test/conformance/fixture"
)

func main() {
	uri := flag.String("uri", "mongodb://127.0.0.1:27017", "local MongoDB uri")
	drop := flag.Bool("drop", false, "only drop the fixture databases")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	if *drop {
		err = fixture.Drop(ctx, *uri)
	} else {
		err = fixture.Load(ctx, *uri)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}
