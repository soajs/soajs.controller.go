package dbconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// Profile is the file SOAJS_PROFILE names: where the provision database lives. It is the same
// file the Node.js gateway reads, a module whose export is a JSON object:
//
//	"use strict";
//	module.exports = {"name": "core_provision", "prefix": "", "servers": [{"host": "...", "port": 27017}], ...};
type Profile struct {
	Name        string                 `json:"name"`
	Prefix      string                 `json:"prefix"`
	Cluster     string                 `json:"cluster"`
	Protocol    string                 `json:"protocol"`
	Servers     []Server               `json:"servers"`
	Credentials *Credentials           `json:"credentials"`
	Streaming   map[string]interface{} `json:"streaming"`
	ExtraParam  map[string]interface{} `json:"extraParam"`
	URLParam    map[string]interface{} `json:"URLParam"`
}

var exportsRe = regexp.MustCompile(`(?s)^\s*(?:["']use strict["']\s*;)?\s*module\.exports\s*=\s*(\{.*\})\s*;?\s*$`)

// LoadProfile reads a profile file.
func LoadProfile(path string) (*Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	m := exportsRe.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("profile %s: expected module.exports = {...}", path)
	}
	dec := json.NewDecoder(bytes.NewReader(m[1]))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("profile %s: name is required", path)
	}
	if len(p.ExtraParam) > 0 {
		return nil, fmt.Errorf("profile %s: extraParam must be empty", path)
	}
	return &p, nil
}

// ProvisionDB is coreDB.provision built from the profile.
func (p *Profile) ProvisionDB(env string, timeLoaded int64) *DB {
	return &DB{
		Prefix:      p.Prefix,
		Cluster:     p.Cluster,
		Protocol:    p.Protocol,
		Servers:     p.Servers,
		Credentials: p.Credentials,
		Streaming:   p.Streaming,
		URLParam:    p.URLParam,
		Name:        p.Name,
		RegistryLocation: &Location{
			L1:         "coreDB",
			L2:         "provision",
			Env:        env,
			TimeLoaded: timeLoaded,
		},
	}
}
