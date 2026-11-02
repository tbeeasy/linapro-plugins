package eval360plugin

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
