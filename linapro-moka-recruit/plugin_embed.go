// Package mokarecruit 是 linapro-moka-recruit 源码插件的模块根包。它只承载
// 供宿主消费的嵌入 manifest/前端资源；所有运行时逻辑位于 backend/ 下。
package mokarecruit

import "embed"

//go:embed plugin.yaml frontend manifest
var EmbeddedFiles embed.FS
