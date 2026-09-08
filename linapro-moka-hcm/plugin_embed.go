// Package mokahcmplugin 是 linapro-moka-hcm 源插件的模块根包。
// 它仅承载宿主使用的内嵌 manifest/frontend 资源；
// 所有运行时逻辑位于 backend/ 下。
package mokahcmplugin

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
