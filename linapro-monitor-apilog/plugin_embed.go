package pluginmonitorapilog

import "embed"

// EmbeddedFiles 包含插件清单、按约定组织的 SQL 资产与前端源码资源。
//
//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
