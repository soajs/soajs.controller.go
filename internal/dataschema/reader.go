package dataschema

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"go.mongodb.org/mongo-driver/bson"
)

// Action is what the gateway did with a problem in a document (docs/data-schema/imperfect-data.md).
type Action string

const (
	// ReadAsNode: the value is read the way the Node.js gateway reads it.
	ReadAsNode Action = "read-as-node"
	// IgnoredField: a field the spec does not list, which the gateway does not read.
	IgnoredField Action = "ignored field"
	// DroppedPiece: the smallest piece holding the problem is left out; the rest is read.
	DroppedPiece Action = "dropped piece"
	// OldShape: a piece in a shape Node.js still reads but this spec does not; it is left out
	// toward refusal.
	OldShape Action = "old shape"
	// SkippedDocument: the document cannot be read at all.
	SkippedDocument Action = "skipped document"
	// DataProblem: the document matches the schema but refers to something that does not exist.
	DataProblem Action = "data problem"
	// FellBackToKeyPackage: a user's roles could not be read (its groups, a group, or a group's
	// allowedPackages entry); as Node.js, they give no packages, and the key's package ACL applies
	// when nothing else gives one.
	FellBackToKeyPackage Action = "fell back to key package"
)

// Finding is one problem in one document and what the gateway did with it. It names locations
// only, never values.
type Finding struct {
	Collection string `json:"collection"`
	ID         string `json:"_id"`
	Path       string `json:"path"`
	Rule       string `json:"rule"`
	Action     Action `json:"action"`
	// Piece is the JSON pointer of what was left out, when it is not Path itself.
	Piece string `json:"piece,omitempty"`
}

// Policy is how one collection is read when a document does not match its schema.
type Policy interface {
	// Prepare applies the Node.js readings and leaves out old shapes, before the schema is checked.
	Prepare(r *Reading)
	// Unit is the piece to leave out for a problem at path, or nil when the whole document
	// cannot be read.
	Unit(r *Reading, path []string) []string
	// Closed reports whether fields the spec does not list are refused at path rather than
	// ignored.
	Closed(path []string) bool
}

// Reading is one document being read.
type Reading struct {
	Collection string
	ID         string
	Doc        map[string]interface{}
	Findings   []Finding
	// Raw are the values a Node.js reading replaced, by JSON pointer.
	Raw map[string]interface{}
	// onRemove sees each piece just before it is left out.
	onRemove func(r *Reading, piece []string)
}

// Result is what reading one document gave.
type Result struct {
	Collection string
	// ID is the document's _id: the hex of an ObjectId, else the value quoted, so a string _id
	// never takes an ObjectId's place.
	ID       string
	Findings []Finding
	// OK: the document was read, possibly in part, and decoded.
	OK bool
	// Raw are the values a Node.js reading replaced, by JSON pointer: where Node.js passes a value
	// through (injected or dumped), the gateway passes the raw one, and decides on the reading.
	Raw map[string]interface{}
}

// Get returns the value at path, and whether it is there.
func (r *Reading) Get(path ...string) (interface{}, bool) {
	var cur interface{} = r.Doc
	for _, p := range path {
		switch t := cur.(type) {
		case map[string]interface{}:
			v, ok := t[p]
			if !ok {
				return nil, false
			}
			cur = v
		case []interface{}:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// Set replaces the value at path, whose parent must exist.
func (r *Reading) Set(path []string, v interface{}) {
	if len(path) == 0 {
		return
	}
	parent, ok := r.Get(path[:len(path)-1]...)
	if !ok {
		return
	}
	last := path[len(path)-1]
	switch t := parent.(type) {
	case map[string]interface{}:
		t[last] = v
	case []interface{}:
		if i, err := strconv.Atoi(last); err == nil && i >= 0 && i < len(t) {
			t[i] = v
		}
	}
}

// ReadAs records a Node.js reading of the value at path; the caller has applied it.
func (r *Reading) ReadAs(path []string, rule string) {
	r.add(path, rule, ReadAsNode, nil)
}

// Drop leaves out the piece at path, recording why.
func (r *Reading) Drop(path []string, rule string, action Action) {
	r.add(path, rule, action, nil)
	r.remove([][]string{path})
}

func (r *Reading) add(path []string, rule string, action Action, piece []string) {
	f := Finding{Collection: r.Collection, ID: r.ID, Path: Pointer(redact(r.Collection, path)), Rule: rule, Action: action}
	if piece != nil && Pointer(piece) != Pointer(path) {
		f.Piece = Pointer(redact(r.Collection, piece))
	}
	r.Findings = append(r.Findings, f)
}

// remove deletes the pieces, array items from the highest index down so the others keep theirs.
func (r *Reading) remove(pieces [][]string) {
	sort.SliceStable(pieces, func(i, j int) bool { return pathAfter(pieces[i], pieces[j]) })
	for _, p := range pieces {
		if len(p) == 0 {
			continue
		}
		parent, ok := r.Get(p[:len(p)-1]...)
		if !ok {
			continue
		}
		if r.onRemove != nil {
			r.onRemove(r, p)
		}
		last := p[len(p)-1]
		switch t := parent.(type) {
		case map[string]interface{}:
			delete(t, last)
		case []interface{}:
			i, err := strconv.Atoi(last)
			if err != nil || i < 0 || i >= len(t) {
				continue
			}
			r.Set(p[:len(p)-1], append(t[:i:i], t[i+1:]...))
		}
	}
}

// pathAfter orders deeper paths and higher array indexes first.
func pathAfter(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		ai, aerr := strconv.Atoi(a[i])
		bi, berr := strconv.Atoi(b[i])
		if aerr == nil && berr == nil {
			return ai > bi
		}
		return a[i] > b[i]
	}
	return len(a) > len(b)
}

// redact hides the map keys that are keys: a user's config.keys are keyed by internal key.
func redact(coll string, path []string) []string {
	if coll == "users" && len(path) >= 3 && path[0] == "config" && path[1] == "keys" {
		out := append([]string{}, path...)
		out[2] = "<redacted>"
		return out
	}
	return path
}

// Pointer is the JSON pointer of path.
func Pointer(path []string) string {
	var b strings.Builder
	for _, p := range path {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(p, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// Segments is the path of a JSON pointer.
func Segments(pointer string) []string {
	if pointer == "" || pointer == "/" {
		return []string{}
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}
	return parts
}

// HasPrefix reports whether path starts with prefix.
func HasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

// maxPasses bounds the validate and leave-out passes over one document.
const maxPasses = 64

// Read reads a raw BSON document through the policy: the Node.js readings first, then every
// piece that still does not match is left out, the smallest first, and fields the spec does not
// list are ignored. It decodes what is left into out. A document that cannot be read, or decoded,
// is not OK and out is untouched; the findings say why, with the _id. They name locations only.
func (c *Collection) Read(raw bson.Raw, p Policy, out interface{}) Result {
	return c.ReadWith(raw, p, out, nil)
}

// ReadWith is Read with a function that sees each piece just before it is left out, with the
// document as it is then.
func (c *Collection) ReadWith(raw bson.Raw, p Policy, out interface{}, onRemove func(r *Reading, piece []string)) Result {
	r := &Reading{Collection: c.Name, ID: DocumentID(raw), Raw: map[string]interface{}{}, onRemove: onRemove}
	res := func(ok bool) Result {
		return Result{Collection: c.Name, ID: r.ID, Findings: r.Findings, OK: ok, Raw: r.Raw}
	}
	ext, err := bson.MarshalExtJSON(raw, false, false)
	if err != nil {
		r.add(nil, "not readable as BSON", SkippedDocument, nil)
		return res(false)
	}
	var v interface{}
	if err := json.Unmarshal(ext, &v); err != nil {
		r.add(nil, "not readable as JSON", SkippedDocument, nil)
		return res(false)
	}
	doc, isMap := v.(map[string]interface{})
	if !isMap {
		r.add(nil, "not a document", SkippedDocument, nil)
		return res(false)
	}
	r.Doc = doc
	if !c.read(r, p) {
		return res(false)
	}
	plain, err := json.Marshal(Plain(r.Doc))
	if err != nil {
		r.add(nil, "not encodable", SkippedDocument, nil)
		return res(false)
	}
	if err := json.Unmarshal(plain, out); err != nil {
		// a value the schema allows but the gateway's type does not: a gap in the schema
		r.add(nil, "decode: "+decodeError(err), SkippedDocument, nil)
		return res(false)
	}
	return res(true)
}

// decodeError names the field and the types of a decode error, never the value.
func decodeError(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return "the value of " + te.Field + " is a JSON " + te.Value + ", the gateway reads a " + te.Type.String()
	}
	return "the document does not decode"
}

func (c *Collection) read(r *Reading, p Policy) bool {
	p.Prepare(r)
	for pass := 0; pass < maxPasses; pass++ {
		err := c.schema.Validate(r.Doc)
		if err == nil {
			return true
		}
		verr, isVerr := err.(*jsonschema.ValidationError)
		if !isVerr {
			r.add(nil, err.Error(), SkippedDocument, nil)
			return false
		}
		unknown, broken := c.classify(r, verr, p)
		changed := false
		var pieces [][]string
		seen := map[string]bool{}
		for _, u := range unknown {
			obj, _ := r.Get(u.path...)
			m, _ := obj.(map[string]interface{})
			for _, name := range sortedKeys(m) {
				if u.known[name] {
					continue
				}
				field := append(append([]string{}, u.path...), name)
				if seen[Pointer(field)] {
					continue
				}
				seen[Pointer(field)] = true
				r.add(field, u.rule, IgnoredField, nil)
				pieces = append(pieces, field)
			}
		}
		for _, b := range broken {
			unit := p.Unit(r, b.path)
			if unit == nil {
				r.add(b.path, b.rule, SkippedDocument, nil)
				return false
			}
			if _, present := r.Get(unit...); len(unit) == 0 || !present {
				// nothing to leave out: a field the document needs is missing
				r.add(b.path, b.rule, SkippedDocument, nil)
				return false
			}
			key := Pointer(unit)
			r.add(b.path, b.rule, DroppedPiece, unit)
			if seen[key] {
				continue
			}
			seen[key] = true
			pieces = append(pieces, unit)
		}
		pieces = outermost(pieces)
		if len(pieces) > 0 {
			r.remove(pieces)
			changed = true
		}
		if !changed {
			r.add(nil, "no piece to leave out", SkippedDocument, nil)
			return false
		}
	}
	r.add(nil, "too many problems", SkippedDocument, nil)
	return false
}

// outermost keeps the pieces that are not inside another one.
func outermost(pieces [][]string) [][]string {
	var out [][]string
	for i, p := range pieces {
		inside := false
		for j, q := range pieces {
			if i != j && len(q) < len(p) && HasPrefix(p, q) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, p)
		}
	}
	return out
}

type problem struct {
	path []string
	rule string
}

type unknownFields struct {
	path  []string
	rule  string
	known map[string]bool
}

// classify splits the leaf errors into fields the spec does not list (an additionalProperties
// false, outside a closed place) and broken values. A oneOf or anyOf that fails is one broken
// value at its own location, unless one of its branches fails only on unknown fields: that
// branch is taken and those fields are ignored.
func (c *Collection) classify(r *Reading, verr *jsonschema.ValidationError, p Policy) (unknown []unknownFields, broken []problem) {
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		kw := lastSegment(e.KeywordLocation)
		if kw == "oneOf" || kw == "anyOf" {
			for _, branch := range e.Causes {
				if u, onlyUnknown := c.onlyUnknown(branch, p); onlyUnknown && len(u) > 0 {
					unknown = append(unknown, u...)
					return
				}
			}
			broken = append(broken, problem{Segments(e.InstanceLocation), e.KeywordLocation})
			return
		}
		if len(e.Causes) == 0 {
			if u, isUnknown := c.unknownAt(e, p); isUnknown {
				unknown = append(unknown, u)
				return
			}
			if kw == "required" {
				if missing := c.missing(r, e); len(missing) > 0 {
					broken = append(broken, missing...)
					return
				}
			}
			broken = append(broken, problem{Segments(e.InstanceLocation), e.KeywordLocation})
			return
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(verr)
	return unknown, broken
}

// missing is one problem per required property absent from the object, at the property's path.
func (c *Collection) missing(r *Reading, e *jsonschema.ValidationError) []problem {
	frag := e.AbsoluteKeywordLocation
	if i := strings.Index(frag, "#"); i >= 0 {
		frag = frag[i+1:]
	}
	names, _ := lookup(c.raw, Segments(frag))
	list, _ := names.([]interface{})
	path := Segments(e.InstanceLocation)
	obj, _ := r.Get(path...)
	m, isMap := obj.(map[string]interface{})
	if !isMap {
		return nil
	}
	var out []problem
	for _, n := range list {
		name, _ := n.(string)
		if _, present := m[name]; !present && name != "" {
			out = append(out, problem{append(append([]string{}, path...), name), e.KeywordLocation})
		}
	}
	return out
}

func (c *Collection) onlyUnknown(e *jsonschema.ValidationError, p Policy) ([]unknownFields, bool) {
	if len(e.Causes) == 0 {
		u, ok := c.unknownAt(e, p)
		if !ok {
			return nil, false
		}
		return []unknownFields{u}, true
	}
	var all []unknownFields
	for _, cause := range e.Causes {
		u, ok := c.onlyUnknown(cause, p)
		if !ok {
			return nil, false
		}
		all = append(all, u...)
	}
	return all, true
}

// unknownAt reports an additionalProperties false at an object the policy does not close, with
// the properties the schema lists there.
func (c *Collection) unknownAt(e *jsonschema.ValidationError, p Policy) (unknownFields, bool) {
	if lastSegment(e.KeywordLocation) != "additionalProperties" {
		return unknownFields{}, false
	}
	path := Segments(e.InstanceLocation)
	if p.Closed(path) {
		return unknownFields{}, false
	}
	frag := e.AbsoluteKeywordLocation
	if i := strings.Index(frag, "#"); i >= 0 {
		frag = frag[i+1:]
	}
	at := Segments(frag)
	if len(at) == 0 {
		return unknownFields{}, false
	}
	value, found := lookup(c.raw, at)
	if !found || value != false {
		return unknownFields{}, false
	}
	parent, _ := lookup(c.raw, at[:len(at)-1])
	known := map[string]bool{}
	if obj, isObj := parent.(map[string]interface{}); isObj {
		if props, isProps := obj["properties"].(map[string]interface{}); isProps {
			for k := range props {
				known[k] = true
			}
		}
	}
	return unknownFields{path: path, rule: e.KeywordLocation, known: known}, true
}

func lookup(v interface{}, path []string) (interface{}, bool) {
	cur := v
	for _, p := range path {
		switch t := cur.(type) {
		case map[string]interface{}:
			next, ok := t[p]
			if !ok {
				return nil, false
			}
			cur = next
		case []interface{}:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func lastSegment(pointer string) string {
	if i := strings.LastIndex(pointer, "/"); i >= 0 {
		return pointer[i+1:]
	}
	return pointer
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DocumentID is the document's _id: the hex of an ObjectId, else the value quoted, so a string
// _id never takes an ObjectId's key.
func DocumentID(raw bson.Raw) string {
	v, err := raw.LookupErr("_id")
	if err != nil {
		return ""
	}
	if oid, ok := v.ObjectIDOK(); ok {
		return oid.Hex()
	}
	if s, ok := v.StringValueOK(); ok {
		return strconv.Quote(s)
	}
	return v.String()
}

// Truthy is JavaScript truthiness for a decoded JSON value.
func Truthy(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case json.Number:
		f, err := t.Float64()
		return err != nil || f != 0
	case map[string]interface{}:
		// extended JSON numbers ({"$numberLong": "0"}) and dates are objects in Go, values in Node.js
		if n, ok := t["$numberInt"].(string); ok && len(t) == 1 {
			return n != "0"
		}
		if n, ok := t["$numberLong"].(string); ok && len(t) == 1 {
			return n != "0"
		}
		if n, ok := t["$numberDouble"].(string); ok && len(t) == 1 {
			return n != "0" && n != "0.0" && n != "-0.0" && n != "NaN"
		}
		return true
	}
	return true
}

// NoteBool records that a value the gateway reads with JavaScript truthiness is not a boolean.
// The value is left as it is: the gateway passes it through where Node.js does, and decides on its
// truthiness.
func (r *Reading) NoteBool(path []string, nodeRef string) {
	v, ok := r.Get(path...)
	if !ok {
		return
	}
	if _, isBool := v.(bool); isBool {
		return
	}
	r.ReadAs(path, nodeRef)
}

// ReadBool reads the value at path as a JavaScript boolean when it is not already one, recording
// the reading with the Node.js line it follows. A missing value is left missing.
func (r *Reading) ReadBool(path []string, nodeRef string) {
	v, ok := r.Get(path...)
	if !ok {
		return
	}
	if _, isBool := v.(bool); isBool {
		return
	}
	r.Raw[Pointer(path)] = v
	r.Set(path, Truthy(v))
	r.ReadAs(path, nodeRef)
}
