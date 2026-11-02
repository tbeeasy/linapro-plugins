// Package pluginemployeecore 嵌入 linapro-employee-core 插件清单、
// 生命周期 SQL 资产与后台页面资源，用于数据源插件注册。
// 除能力（员工身份、飞书登录、与考勤/问卷共享的 EmpAuth 会话接缝）外，
// 本插件还提供后台「通讯录管理 → 员工列表」页面。
package pluginemployeecore

import "embed"

// EmbeddedFiles 包含插件清单、约定式的 SQL / i18n 资产，以及后台页面。
//
//go:embed plugin.yaml manifest frontend
var EmbeddedFiles embed.FS
