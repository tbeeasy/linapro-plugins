// Package larkpush 是 linapro-lark-push 能力插件的模块根包。
// 它只承载宿主机消费的嵌入 manifest/前端资产；所有运行逻辑位于 backend/ 目录下。
package larkpush

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
