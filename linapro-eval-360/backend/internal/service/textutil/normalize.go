// Package textutil 提供 360 测评插件内共享的纯文本归一化工具。
//
// 只放「无副作用、无外部依赖」的字符串处理——查重查询、唯一索引冲突兜底、
// 业务错误码等**判定策略**留在各自的 service 包内，避免规则与策略互相牵连。
package textutil

import (
	"strings"
)

// NormalizeName 把名称类字段（项目名 / 问卷标题 / 角色名）按以下规则归一化，
// 作为「精确匹配」的前置——不做同义词猜测，只消除空白差异导致的重复：
//   - 全角空格（U+3000）转半角空格；
//   - 折叠内部连续空白（含 tab / 换行）为单个半角空格；
//   - 去首尾空白。
//
// 大小写**不折叠**（名称大小写可能有意区分）。纯空白输入归一化后为空串，
// 由调用方决定空串是否非法。
func NormalizeName(name string) string {
	// 全角空格转半角，使其能被 strings.Fields 识别为空白。
	name = strings.ReplaceAll(name, "　", " ")
	// 按空白切分后再用单空格拼接，实现内部连续空白的折叠。
	fields := strings.Fields(name)
	return strings.Join(fields, " ")
}

// NormalizeEmployeeNo 把工号做轻量归一：全角空格转半角 + 去首尾空白。
// 工号内部空白不做折叠（工号本身不含空白，折叠反而会掩盖录入错误），
// 也不做大小写折叠（工号大小写可能区分）。
func NormalizeEmployeeNo(no string) string {
	no = strings.ReplaceAll(no, "　", " ")
	return strings.TrimSpace(no)
}
