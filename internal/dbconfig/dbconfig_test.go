package dbconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestURI(t *testing.T) {
	tests := []struct {
		name string
		db   DB
		want string
	}{
		{"one server, no auth", DB{Name: "p", Servers: []Server{{Host: "127.0.0.1", Port: 27017}}, URLParam: map[string]interface{}{}},
			"mongodb://127.0.0.1:27017/"},
		{"replica set", DB{Name: "p", Servers: []Server{{Host: "a", Port: 1}, {Host: "b", Port: 2}}},
			"mongodb://a:1,b:2/"},
		{"srv with credentials and options", DB{Name: "p", Protocol: "mongodb+srv://", Servers: []Server{{Host: "c.example.com"}},
			Credentials: &Credentials{Username: "u", Password: "p@ss"},
			URLParam:    map[string]interface{}{"maxpoolsize": float64(5), "retryWrites": true, "writeConcern": map[string]interface{}{"w": "majority"}}},
			"mongodb+srv://u:p%40ss@c.example.com/?maxpoolsize=5&retryWrites=true&w=majority"},
	}
	for _, tt := range tests {
		got, err := tt.db.URI()
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestClientOptionsRefusesUnknownOptions(t *testing.T) {
	db := DB{Name: "p", Servers: []Server{{Host: "127.0.0.1", Port: 27017}}, URLParam: map[string]interface{}{"useUnifiedTopology": true}}
	_, err := db.ClientOptions()
	if err == nil || !strings.Contains(err.Error(), "useunifiedtopology") {
		t.Fatalf("got %v, want an error naming useunifiedtopology", err)
	}
	db.URLParam = map[string]interface{}{"MaxPoolSize": float64(10), "retryWrites": true}
	if _, err := db.ClientOptions(); err != nil {
		t.Fatalf("known options refused: %v", err)
	}
}

func TestCredentialsNeedBothFields(t *testing.T) {
	db := DB{Name: "p", Servers: []Server{{Host: "h"}}, Credentials: &Credentials{Username: ""}}
	if _, err := db.URI(); err == nil {
		t.Fatal("empty credentials accepted")
	}
}

func TestLoadProfile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p, err := LoadProfile(write("ok", `"use strict";
module.exports = {"name":"core_provision","prefix":"t_","servers":[{"host":"127.0.0.1","port":27017}],"credentials":null,"streaming":{},"extraParam":{},"URLParam":{}};`))
	if err != nil {
		t.Fatal(err)
	}
	db := p.ProvisionDB("dev", 1)
	if db.DatabaseName() != "t_core_provision" || db.Credentials != nil || db.RegistryLocation.L2 != "provision" {
		t.Errorf("unexpected provision db %+v", db)
	}
	if _, err := LoadProfile(write("unknown", `module.exports = {"name":"x","servers":[],"clusters":{}};`)); err == nil {
		t.Error("unknown field accepted")
	}
	if _, err := LoadProfile(write("extra", `module.exports = {"name":"x","servers":[],"extraParam":{"db":{}}};`)); err == nil {
		t.Error("extraParam option groups accepted")
	}
	if _, err := LoadProfile(write("notjs", `{"name":"x"}`)); err == nil {
		t.Error("a file without module.exports accepted")
	}
}

// The compose profile (deployments/profile.js) loads.
func TestComposeProfile(t *testing.T) {
	p, err := LoadProfile("../../deployments/profile.js")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "core_provision" || len(p.Servers) != 1 || p.Servers[0].Host != "mongo" || p.Servers[0].Port != 27017 {
		t.Errorf("profile %+v", p)
	}
}
