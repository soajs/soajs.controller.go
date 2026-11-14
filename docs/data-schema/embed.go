// Package schemas embeds the data schema, so the gateway validates every document it reads
// against the same files the documentation describes.
package schemas

import "embed"

// FS holds every *.schema.json of this directory.
//
//go:embed *.schema.json
var FS embed.FS
