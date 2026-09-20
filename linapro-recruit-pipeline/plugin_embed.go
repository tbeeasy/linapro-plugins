// Package recruitpipeline 是 linapro-recruit-pipeline 源插件的模块根包。
// 它只携带宿主消费的嵌入 manifest/frontend 资产；所有运行时逻辑位于 backend/ 下。
package recruitpipeline

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
