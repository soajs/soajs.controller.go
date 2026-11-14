// Command mockservice is a service of the fixture: it answers every call on its service port with
// the method, path and query it received, its body when it has one, and the soajsinjectobj the
// gateway sent. Its maintenance port answers GET /mcp/tools with the service's tools (-tools
// soajs: a soajs tool list; swagger: an OpenAPI document as YAML) and anything else as the
// heartbeat.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 24100, "service port")
	maintenancePort := flag.Int("maintenance", 25100, "maintenance port")
	tools := flag.String("tools", "", "the tools GET /mcp/tools answers: soajs or swagger")
	flag.Parse()

	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *maintenancePort), http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mcp/tools" {
					switch *tools {
					case "soajs":
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, soajsTools)
						return
					case "swagger":
						write(w, map[string]interface{}{"result": true, "data": map[string]interface{}{"swagger": swaggerTools}})
						return
					}
				}
				write(w, map[string]interface{}{"result": true})
			})))
	}()
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// the soajsinjectobj the gateway sent, parsed: written back in a canonical key order
			var inject interface{}
			if h := r.Header.Get("soajsinjectobj"); h != "" {
				if err := json.Unmarshal([]byte(h), &inject); err != nil {
					inject = "unparsable: " + h
				}
			}
			data := map[string]interface{}{
				"method": r.Method,
				"path":   r.URL.Path,
				"query":  r.URL.RawQuery,
				"inject": inject,
			}
			if body, _ := io.ReadAll(r.Body); len(body) > 0 {
				data["body"] = string(body)
			}
			write(w, map[string]interface{}{"result": true, "data": data})
		})))
}

func write(w http.ResponseWriter, body interface{}) {
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// soajsTools are the tools a soajs service builds from its config.js: label, method and route,
// the schema of its inputs with their validation description, and where each input goes.
const soajsTools = `{"result":true,"data":{"tools":[
{"m":"get","v":"/public/hello","l":"Say hello","group":"Hello","inputSchema":{"type":"object","properties":{"name":{"type":"string","description":"Who to greet"},"tags":{"type":"array","items":{"type":"string"}}}},"fields":{"name":{"in":"query","path":["name"]},"tags":{"in":"query","path":["tags"]}}},
{"m":"get","v":"/private/hello","l":"Say hello to a signed-in user","group":"Hello","inputSchema":{"type":"object","properties":{}},"fields":{}},
{"m":"post","v":"/private/item","l":"Create an item","group":"Item","inputSchema":{"type":"object","properties":{"name":{"type":"string","description":"The item's name"},"size":{"type":"object","properties":{"w":{"type":"integer"}}}},"required":["name"]},"fields":{"name":{"in":"body","path":["name"]},"size":{"in":"body","path":["dim","size"]}}},
{"m":"put","v":"/private/item/:id","operationId":"updateItem","l":"Update an item","group":"Item","inputSchema":{"type":"object","properties":{"id":{"type":"string","description":"The item's id"},"name":{"type":"string"}},"required":["id"]},"fields":{"id":{"in":"params","path":["id"]},"name":{"in":"body","path":["name"]}}},
{"m":"patch","v":"/private/item/:id","l":"Patch an item","group":"Item","inputSchema":{"type":"object","properties":{"id":{"type":"string"}}},"fields":{"id":{"in":"params","path":["id"]}}},
{"m":"delete","v":"/private/item/:id","l":"","group":"Item","inputSchema":{"type":"object","properties":{"id":{"type":"string"},"force":{"type":"boolean"}}},"fields":{"id":{"in":"params","path":["id"]},"force":{"in":"query","path":["force"]}}},
{"m":"get","v":"/private/admin","l":"Not in the package","inputSchema":{"type":"object","properties":{}},"fields":{}},
{"m":"get","l":"No route"}
]}}`

// swaggerTools is the OpenAPI document of a service built with an SDK.
const swaggerTools = `openapi: 3.0.1
info:
  title: other
  version: "1"
paths:
  /throttled:
    get:
      operationId: throttledCall
      summary: A throttled call
  /hello:
    parameters:
      - name: lang
        in: query
        schema:
          type: string
        description: The language
    get:
      summary: Hello from the other service
      tags: [Hello]
      parameters:
        - $ref: '#/components/parameters/Polite'
    post:
      summary: Not in the package
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Greeting'
components:
  parameters:
    Polite:
      name: polite
      in: query
      required: true
      schema:
        type: boolean
        nullable: true
  schemas:
    Greeting:
      type: object
      required: [text, id]
      properties:
        id:
          type: string
          readOnly: true
        text:
          type: string
          example: hi
`
