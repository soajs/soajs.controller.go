package urac

import "sort"

// ResolveMembership is the membership the user holds for the request's product (lib/membership.js):
// the custom registry gateway membership block maps a main product to its sub products; a request
// under the main product or one of its sub products reads the user's entry for the main product.
// It fails closed: an inactive block, an unconfigured product or no entry resolve to "".
func ResolveMembership(config map[string]interface{}, product string, memberships interface{}) string {
	if config == nil || !truthy(config["active"]) {
		return ""
	}
	list, ok := memberships.([]interface{})
	if product == "" || !ok || len(list) == 0 {
		return ""
	}
	// Object.keys order: the block is a JSON object, its keys are read in document order by
	// Node.js; Go maps have none, so a product configured under two main products is ambiguous
	// in both and resolved here by the main product's name.
	keys := make([]string, 0, len(config))
	for k := range config {
		if k != "active" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	main := ""
	for _, k := range keys {
		if k == product {
			main = k
			break
		}
		if subs, ok := config[k].([]interface{}); ok {
			for _, s := range subs {
				if s == product {
					main = k
					break
				}
			}
		}
		if main != "" {
			break
		}
	}
	if main == "" {
		return ""
	}
	for _, e := range list {
		entry, _ := e.(map[string]interface{})
		if entry != nil && entry["product"] == main {
			if m, ok := entry["membership"].(string); ok && m != "" {
				return m
			}
		}
	}
	return ""
}
