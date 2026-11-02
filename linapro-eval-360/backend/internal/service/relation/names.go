// names.go 保留本包内的名称归一化别名，指向共享工具包 textutil。
//
// 归一化实现已上收到 backend/internal/service/textutil（纯字符串工具，无副作用），
// 这里只做一层薄别名：让包内调用点保持既有函数名，同时避免同一规则在多个包各写一份。
package relation

import "lina-plugin-linapro-eval-360/backend/internal/service/textutil"

// NormalizeRoleName 见 textutil.NormalizeName：全角空格转半角 + 折叠内部空白 + 去首尾空白。
// 角色名归一化是「精确匹配」的前置——撞名视为同一角色，不做同义词猜测。
func NormalizeRoleName(name string) string {
	return textutil.NormalizeName(name)
}

// NormalizeEmployeeNo 见 textutil.NormalizeEmployeeNo：全角空格转半角 + 去首尾空白。
func NormalizeEmployeeNo(no string) string {
	return textutil.NormalizeEmployeeNo(no)
}
