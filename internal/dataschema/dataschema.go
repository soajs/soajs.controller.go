// Package dataschema validates MongoDB documents against the data schema of the gateway
// (docs/data-schema). Each collection has one JSON Schema, the latest shape only.
//
// A document is converted to relaxed extended JSON before it is validated, so an ObjectId is
// {"$oid": "..."} and a date is {"$date": "..."}, as the schemas describe them.
package dataschema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"go.mongodb.org/mongo-driver/bson"

	schemas "github.com/soajs/soajs.controller.go/docs/data-schema"
)

// Database kinds a collection lives in.
const (
	// DBProvision is the core provision database: the profile's name with the profile's prefix.
	DBProvision = "provision"
	// DBOAuth is the environment's oauth database, else the provision database.
	DBOAuth = "oauth"
	// DBUrac is a tenant urac database: <tenant code>_urac with the environment prefix.
	DBUrac = "urac"
	// DBGateway is the environment's gateway database.
	DBGateway = "gateway"
)

// Collection is one collection and its schema.
type Collection struct {
	Name     string
	Database string
	File     string
	schema   *jsonschema.Schema
}

// meta is the x-soajs block every schema file carries.
type meta struct {
	XSoajs struct {
		Collection string `json:"collection"`
		Database   string `json:"database"`
	} `json:"x-soajs"`
}

// DefaultDir is docs/data-schema in this repository.
func DefaultDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "docs", "data-schema")
}

var (
	embedded     map[string]*Collection
	embeddedErr  error
	embeddedOnce sync.Once
)

// Embedded returns the schemas built into the binary, by collection name.
func Embedded() (map[string]*Collection, error) {
	embeddedOnce.Do(func() {
		var list []*Collection
		list, embeddedErr = LoadFS(schemas.FS)
		if embeddedErr != nil {
			return
		}
		embedded = map[string]*Collection{}
		for _, c := range list {
			embedded[c.Name] = c
		}
	})
	return embedded, embeddedErr
}

// Load compiles every *.schema.json in dir.
func Load(dir string) ([]*Collection, error) {
	return LoadFS(os.DirFS(dir))
}

// LoadFS compiles every *.schema.json in fsys.
func LoadFS(fsys fs.FS) ([]*Collection, error) {
	files, err := fs.Glob(fsys, "*.schema.json")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no *.schema.json found")
	}
	sort.Strings(files)
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	var collections []*Collection
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var m meta
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if m.XSoajs.Collection == "" || m.XSoajs.Database == "" {
			return nil, fmt.Errorf("%s: x-soajs.collection and x-soajs.database are required", filepath.Base(f))
		}
		url := "mem:///" + path.Base(f)
		if err := compiler.AddResource(url, strings.NewReader(string(raw))); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		s, err := compiler.Compile(url)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		collections = append(collections, &Collection{
			Name:     m.XSoajs.Collection,
			Database: m.XSoajs.Database,
			File:     filepath.Base(f),
			schema:   s,
		})
	}
	return collections, nil
}

// Problem is where a document breaks the schema. It names locations only, never values.
type Problem struct {
	// Instance is the JSON pointer of the offending field in the document.
	Instance string
	// Rule is the JSON pointer of the schema keyword that failed.
	Rule string
}

// ValidateBSON validates a raw BSON document.
func (c *Collection) ValidateBSON(raw bson.Raw) ([]Problem, error) {
	ext, err := bson.MarshalExtJSON(raw, false, false)
	if err != nil {
		return nil, err
	}
	return c.ValidateJSON(ext)
}

// ValidateJSON validates a document in relaxed extended JSON.
func (c *Collection) ValidateJSON(doc []byte) ([]Problem, error) {
	var v interface{}
	if err := json.Unmarshal(doc, &v); err != nil {
		return nil, err
	}
	return c.validate(v)
}

// Decode validates a raw BSON document and, when it matches, decodes it into out through JSON:
// an ObjectId becomes its hex string and a date the form JSON.stringify writes. A document that
// does not match returns its problems and leaves out untouched.
func (c *Collection) Decode(raw bson.Raw, out interface{}) ([]Problem, error) {
	ext, err := bson.MarshalExtJSON(raw, false, false)
	if err != nil {
		return nil, err
	}
	var v interface{}
	if err := json.Unmarshal(ext, &v); err != nil {
		return nil, err
	}
	problems, err := c.validate(v)
	if err != nil || problems != nil {
		return problems, err
	}
	plain, err := json.Marshal(Plain(v))
	if err != nil {
		return nil, err
	}
	return nil, json.Unmarshal(plain, out)
}

// Plain turns relaxed extended JSON into plain JSON values: {"$oid": h} becomes h and
// {"$date": d} the UTC time with milliseconds, as JSON.stringify writes a Date.
func Plain(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 1 {
			if oid, ok := t["$oid"].(string); ok {
				return oid
			}
			if d, ok := t["$date"]; ok {
				if s, ok := d.(string); ok {
					if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
						return tm.UTC().Format("2006-01-02T15:04:05.000Z")
					}
				}
				return d
			}
		}
		for k, e := range t {
			t[k] = Plain(e)
		}
		return t
	case []interface{}:
		for i, e := range t {
			t[i] = Plain(e)
		}
		return t
	}
	return v
}

func (c *Collection) validate(v interface{}) ([]Problem, error) {
	err := c.schema.Validate(v)
	if err == nil {
		return nil, nil
	}
	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return nil, err
	}
	var problems []Problem
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			problems = append(problems, Problem{Instance: e.InstanceLocation, Rule: e.KeywordLocation})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(verr)
	return problems, nil
}
