// Package dataschema validates MongoDB documents against the data schema of the gateway
// (docs/data-schema). Each collection has one JSON Schema, the latest shape only.
//
// A document is converted to relaxed extended JSON before it is validated, so an ObjectId is
// {"$oid": "..."} and a date is {"$date": "..."}, as the schemas describe them.
package dataschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"go.mongodb.org/mongo-driver/bson"
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

// Load compiles every *.schema.json in dir.
func Load(dir string) ([]*Collection, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.schema.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no *.schema.json in %s", dir)
	}
	sort.Strings(files)
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	var collections []*Collection
	for _, f := range files {
		raw, err := os.ReadFile(f)
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
		url := "file://" + filepath.ToSlash(f)
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
