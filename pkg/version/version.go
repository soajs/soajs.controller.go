package version

import (
	_ "embed"
	"encoding/json"
)

//go:embed version.json
var versionJSON []byte

// info holds the parsed version.json data
type info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

var cachedInfo *info

func getInfo() *info {
	if cachedInfo != nil {
		return cachedInfo
	}
	cachedInfo = &info{}
	json.Unmarshal(versionJSON, cachedInfo)
	return cachedInfo
}

// Get returns the current version string
func Get() string {
	return getInfo().Version
}

// Name returns the service name
func Name() string {
	return getInfo().Name
}

// Description returns the service description
func Description() string {
	return getInfo().Description
}
