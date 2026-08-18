// Package mokahcmplugin is the module-root package for the linapro-moka-hcm
// source plugin. It only carries the embedded manifest/frontend assets consumed
// by the host; all runtime logic lives under backend/.
package mokahcmplugin

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
