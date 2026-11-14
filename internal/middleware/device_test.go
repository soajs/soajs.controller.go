package middleware

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/device.json was recorded from the Node.js gateway's securityDeviceCheck
// (soajs.controller mw/mt/utils.js) over device rules and User-Agents; a check that throws in
// Node.js is recorded as 164, the answer Node.js gives.
func TestDeviceCheckParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/device.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string                 `json:"name"`
		Device map[string]interface{} `json:"device"`
		UA     string                 `json:"ua"`
		Code   int                    `json:"code"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := deviceCheck(c.Device, c.UA); got != c.Code {
			t.Errorf("%s / %.50q: got %d, want %d", c.Name, c.UA, got, c.Code)
		}
	}
}
