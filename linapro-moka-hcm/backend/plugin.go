// Package backend is the linapro-moka-hcm source plugin entrypoint. This plugin
// provides the Moka HCM OpenAPI client as a shared library for other plugins.
// It registers no jobs of its own; consuming plugins read HCM credentials from
// host config (keys under plugin.linapro-moka-hcm.hcm.*) and construct a
// *hcm.Client via hcm.NewClient + hcm.NewHCMAuth.
package backend

import (
	"lina-core/pkg/plugin/pluginhost"
	
)

const pluginID = "linapro-moka-hcm"

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(mokahcmplugin.EmbeddedFiles)

	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}
