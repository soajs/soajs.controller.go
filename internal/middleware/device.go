package middleware

import (
	"errors"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/uaparser"
)

// uaParser parses User-Agents for the device check: a few VMs, a bounded cache.
var uaParser = uaparser.New(4, 2048)

// errDeviceRule is a device rule Node.js cannot evaluate either (it throws, and answers 164).
var errDeviceRule = errors.New("invalid device rule")

// deviceCheck is the Node.js securityDeviceCheck (mw/mt/utils.js): the extKey's device deny list
// answers 156 when the User-Agent matches a rule, the allow list when it matches none. It returns
// 0 when the request may go on, and 164 when the User-Agent cannot be parsed or a rule cannot be
// evaluated, as Node.js answers when the check throws.
func deviceCheck(device map[string]interface{}, userAgent string) int {
	if userAgent == "" || device == nil {
		return 0
	}
	deny, hasDeny := device["deny"].([]interface{})
	allow, hasAllow := device["allow"].([]interface{})
	if !hasDeny && !hasAllow {
		return 0
	}
	ua, err := uaParser.Parse(userAgent)
	if err != nil {
		return 164
	}
	if hasDeny {
		denied, err := deviceMatches(deny, ua)
		if err != nil {
			return 164
		}
		if denied {
			return 156
		}
	}
	if hasAllow {
		allowed, err := deviceMatches(allow, ua)
		if err != nil {
			return 164
		}
		if !allowed {
			return 156
		}
	}
	return 0
}

// deviceMatches reports whether any rule matches the User-Agent (checkAccess).
func deviceMatches(rules []interface{}, ua uaparser.UA) (bool, error) {
	if ua.Family == "" {
		return false, nil
	}
	for _, r := range rules {
		if !truthy(r) {
			continue
		}
		// a rule that is not an object has no fields: it matches, as in Node.js
		rule, _ := r.(map[string]interface{})
		match, err := deviceRuleMatches(rule, ua)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

func deviceRuleMatches(rule map[string]interface{}, ua uaparser.UA) (bool, error) {
	if family := rule["family"]; truthy(family) && family != "*" {
		s, ok := family.(string)
		if !ok {
			return false, errDeviceRule
		}
		if !strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(ua.Family)) {
			return false, nil
		}
	}
	if osRule := rule["os"]; truthy(osRule) && osRule != "*" {
		osMap, _ := osRule.(map[string]interface{})
		osFamily, ok := osMap["family"].(string)
		if !ok {
			return false, errDeviceRule
		}
		if ua.OS.Family == "" {
			return false, nil
		}
		if !strings.Contains(strings.ToUpper(strings.TrimSpace(ua.OS.Family)), strings.ToUpper(strings.TrimSpace(osFamily))) {
			return false, nil
		}
		for _, f := range []struct{ name, value string }{{"major", ua.OS.Major}, {"minor", ua.OS.Minor}, {"patch", ua.OS.Patch}} {
			if ok, err := validateField(osMap[f.name], f.value); err != nil || !ok {
				return false, err
			}
		}
	}
	for _, f := range []struct{ name, value string }{{"major", ua.Major}, {"minor", ua.Minor}, {"patch", ua.Patch}} {
		if ok, err := validateField(rule[f.name], f.value); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// validateField compares a version part as Node.js does: a string must equal it (case-insensitive),
// an object's min and max bound it, compared as strings.
func validateField(want interface{}, got string) (bool, error) {
	if !truthy(want) || want == "*" || got == "" {
		return true, nil
	}
	switch w := want.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(w), strings.TrimSpace(got)), nil
	case map[string]interface{}:
		if min := w["min"]; truthy(min) {
			s, ok := min.(string)
			if !ok {
				return false, errDeviceRule
			}
			if strings.TrimSpace(s) > strings.TrimSpace(got) {
				return false, nil
			}
		}
		if max := w["max"]; truthy(max) {
			s, ok := max.(string)
			if !ok {
				return false, errDeviceRule
			}
			if strings.TrimSpace(s) < strings.TrimSpace(got) {
				return false, nil
			}
		}
		return true, nil
	}
	// a number, a boolean or an array has no min or max: it matches, as in Node.js
	return true, nil
}

// truthy is JavaScript truthiness for decoded JSON values.
func truthy(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	}
	return true
}
