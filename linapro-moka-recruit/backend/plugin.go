// Package backend 是 linapro-moka-recruit 源码插件的入口。本插件将 Moka 招聘
// OpenAPI 客户端（Basic Auth + OAuth2）作为共享库提供给其他插件。它自身
// 不注册任何后台任务；业务流水线位于下游插件中。
package backend

import (
	"lina-core/pkg/plugin/pluginhost"
	mokarecruit "lina-plugin-linapro-moka-recruit"
)

const pluginID = "linapro-moka-recruit"

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(mokarecruit.EmbeddedFiles)

	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}
