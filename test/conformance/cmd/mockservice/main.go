// Command mockservice is the conform service of the fixture: it answers every call on its service
// port with the method and path it received, and /heartbeat on its maintenance port.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 24100, "service port")
	maintenancePort := flag.Int("maintenance", 25100, "maintenance port")
	flag.Parse()

	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *maintenancePort), http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				write(w, map[string]interface{}{"result": true})
			})))
	}()
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			write(w, map[string]interface{}{
				"result": true,
				"data": map[string]interface{}{
					"method": r.Method,
					"path":   r.URL.Path,
					"query":  r.URL.RawQuery,
				},
			})
		})))
}

func write(w http.ResponseWriter, body interface{}) {
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
