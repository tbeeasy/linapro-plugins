// Package backend 是 linapro-moka-hcm 源插件入口。该插件
// 将 Moka HCM OpenAPI 客户端作为共享库提供给其他插件使用。
// 它自身不注册任何任务；使用方插件从宿主配置
// （plugin.linapro-moka-hcm.hcm.* 下的键）读取 HCM 凭证，
// 并通过 hcm.NewClient(baseURL, cred) 构建 *hcm.Client。
package backend

import (
	"lina-core/pkg/plugin/pluginhost"
	mokahcmplugin "lina-plugin-linapro-moka-hcm"
)

const pluginID = "linapro-moka-hcm"

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(mokahcmplugin.EmbeddedFiles)

	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}
