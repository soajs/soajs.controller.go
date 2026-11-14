package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// omap is a JSON object that keeps its keys in the order JavaScript gives them (Object.keys):
// integer keys first, ascending, then the others in insertion order. A key set again keeps its
// place. Values are nil, bool, json.Number, string, []interface{} and *omap.
type omap struct {
	keys []string
	m    map[string]interface{}
}

func newOmap() *omap { return &omap{m: map[string]interface{}{}} }

func (o *omap) set(k string, v interface{}) *omap {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
	return o
}

func (o *omap) get(k string) (interface{}, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.m[k]
	return v, ok
}

// has is Object.hasOwnProperty.
func (o *omap) has(k string) bool {
	_, ok := o.get(k)
	return ok
}

func (o *omap) del(k string) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, e := range o.keys {
		if e == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// jsKeys are the keys in JavaScript order.
func (o *omap) jsKeys() []string {
	if o == nil {
		return nil
	}
	var index, named []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			index = append(index, k)
		} else {
			named = append(named, k)
		}
	}
	sort.SliceStable(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 32)
		b, _ := strconv.ParseUint(index[j], 10, 32)
		return a < b
	})
	return append(index, named...)
}

// copy is a shallow copy (Object.assign({}, o)).
func (o *omap) copy() *omap {
	c := newOmap()
	for _, k := range o.jsKeys() {
		c.set(k, o.m[k])
	}
	return c
}

func (o *omap) MarshalJSON() ([]byte, error) { return []byte(jsStringify(o)), nil }

// asOmap is the object a value is, nil for any other value.
func asOmap(v interface{}) *omap {
	o, _ := v.(*omap)
	return o
}

// str is the string a value is, "" for any other value.
func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

var errTrailingData = errors.New("unexpected data after the JSON value")

// parseJSON parses a JSON text as JSON.parse does, objects as *omap, numbers as json.Number.
func parseJSON(data []byte) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errTrailingData
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := newOmap()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.set(kt.(string), v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			list := []interface{}{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := dec.Token()
			return list, err
		}
		return nil, errors.New("unexpected delimiter")
	default:
		return t, nil
	}
}

// parseYAML parses a YAML text as js-yaml load does, to the same values as parseJSON.
func parseYAML(text string) (interface{}, error) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(text), &node); err != nil {
		return nil, err
	}
	if node.Kind == 0 {
		return nil, nil
	}
	return yamlValue(&node)
}

func yamlValue(n *yaml.Node) (interface{}, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return yamlValue(n.Content[0])
	case yaml.AliasNode:
		return yamlValue(n.Alias)
	case yaml.MappingNode:
		o := newOmap()
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, err := yamlValue(n.Content[i])
			if err != nil {
				return nil, err
			}
			v, err := yamlValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			if n.Content[i].Tag == "!!merge" {
				if m := asOmap(v); m != nil {
					for _, mk := range m.jsKeys() {
						if !o.has(mk) {
							o.set(mk, m.m[mk])
						}
					}
				}
				continue
			}
			o.set(jsToString(k), v)
		}
		return o, nil
	case yaml.SequenceNode:
		list := []interface{}{}
		for _, c := range n.Content {
			v, err := yamlValue(c)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	}
	var v interface{}
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	switch t := v.(type) {
	case int:
		return json.Number(strconv.Itoa(t)), nil
	case int64:
		return json.Number(strconv.FormatInt(t, 10)), nil
	case uint64:
		return json.Number(strconv.FormatUint(t, 10)), nil
	case float64:
		return json.Number(jsFormatNumber(t)), nil
	case bool, string, nil:
		return t, nil
	}
	return n.Value, nil
}

// jsFormatNumber is Number.prototype.toString.
func jsFormatNumber(f float64) string {
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		return "null"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// Go writes e-07 and e+21, JavaScript e-7 and e+21
	if i := strings.IndexAny(s, "e"); i != -1 {
		mant, exp := s[:i], s[i+1:]
		sign := exp[0]
		digits := strings.TrimLeft(exp[1:], "0")
		if digits == "" {
			digits = "0"
		}
		s = mant + "e" + string(sign) + digits
	}
	return s
}

// numberValue is the number a JSON number stands for.
func numberValue(n json.Number) float64 {
	f, _ := strconv.ParseFloat(string(n), 64)
	return f
}

// jsStringify is JSON.stringify of a value.
func jsStringify(v interface{}) string {
	var b strings.Builder
	writeJS(&b, v)
	return b.String()
}

func writeJS(b *strings.Builder, v interface{}) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		b.WriteString(jsFormatNumber(numberValue(t)))
	case float64:
		b.WriteString(jsFormatNumber(t))
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case string:
		b.WriteString(jsJSONString(t))
	case []interface{}:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJS(b, e)
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsJSONString(e))
		}
		b.WriteByte(']')
	case *omap:
		b.WriteByte('{')
		for i, k := range t.jsKeys() {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsJSONString(k))
			b.WriteByte(':')
			writeJS(b, t.m[k])
		}
		b.WriteByte('}')
	case map[string]interface{}:
		o := newOmap()
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			o.set(k, t[k])
		}
		writeJS(b, o)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			b.WriteString("null")
			return
		}
		b.Write(raw)
	}
}

// jsToString is String(value).
func jsToString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return jsFormatNumber(numberValue(t))
	case []interface{}:
		parts := make([]string, len(t))
		for i, e := range t {
			if e != nil {
				parts[i] = jsToString(e)
			}
		}
		return strings.Join(parts, ",")
	case *omap:
		return "[object Object]"
	}
	return ""
}

// jsTruthy is a value's truthiness.
func jsTruthy(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case json.Number:
		f := numberValue(t)
		return f != 0 && !math.IsNaN(f)
	}
	return true
}
