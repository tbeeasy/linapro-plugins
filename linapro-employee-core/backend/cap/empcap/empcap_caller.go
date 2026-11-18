// empcap_caller.go 把「谁在调用」写进请求上下文约定变量，供接口请求日志插件
// （linapro-monitor-apilog）读取。
//
// 约定（见 linapro-monitor-apilog design D6）：
// 鉴权中间件通过后执行一次 r.SetCtxVar(CallerCtxKey, <JSON 字符串>)，形如
//
//	{"kind":"emp","id":"5","tenant":1,"name":"张三","no":"GZ001198"}
//
// 为什么用字符串而不是结构体：写入方与读取方不能因此建立 Go 依赖。日志插件一旦依赖
// 员工域，就装不到没有员工端的部署；反过来业务插件也不该依赖日志插件。字符串约定让
// 两边互不依赖——日志插件未安装时，这个变量没有任何读者。
//
// 键名与 JSON 结构只有这一份实现（本文件），两个写入点共用，漂移面不会翻倍。

package empcap

import (
	"encoding/json"
	"strconv"

	"github.com/gogf/gf/v2/net/ghttp"
)

// CallerCtxKey 是写入方与日志插件约定的请求上下文变量键名。
//
// 读取方在 linapro-monitor-apilog 内部持有同值常量副本；本常量的值由单测锁定，
// 两边一起改才会一起失效。
const CallerCtxKey = "linapro.request.caller"

// 调用方类型取值。
const (
	// CallerKindEmp 表示调用方是员工。
	CallerKindEmp = "emp"
)

// callerPayload 是约定变量的 JSON 结构。字段全部可省略，省略即该列为空。
type callerPayload struct {
	// Kind 是调用方类型。
	Kind string `json:"kind"`
	// ID 是 EmpID 的字符串形式，是唯一的机器关联键。
	ID string `json:"id"`
	// Tenant 是租户 ID；无租户语义时省略。
	Tenant int `json:"tenant,omitempty"`
	// Name 是员工姓名快照。
	Name string `json:"name,omitempty"`
	// No 是 Moka 工号快照。
	No string `json:"no,omitempty"`
}

// SetCaller 把员工调用方标识写入请求上下文。
//
// name 与 employeeNo 是**写入时快照**，之后员工改名或换工号都不会回填历史记录——
// 日志回答的是「当时谁调用了这个接口」。
//
// 调用点是鉴权中间件与登录控制器：两处都已握有员工对象，因此这里零额外查询。
func SetCaller(r *ghttp.Request, emp EmpContext, name, employeeNo string) {
	if r == nil {
		return
	}
	payload := callerPayload{
		Kind:   CallerKindEmp,
		ID:     formatEmpID(emp.EmpID),
		Tenant: emp.TenantID,
		Name:   name,
		No:     employeeNo,
	}
	// 序列化只可能是 int64/int/string 字段，不会失败；失败时静默跳过，
	// 让日志那一列为空，而不是让鉴权通过之后抛错。
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	r.SetCtxVar(CallerCtxKey, string(encoded))
}

// formatEmpID 把 EmpID 转成字符串写入约定键：JSON 里统一用字符串，避免 64 位整数
// 在 JavaScript 侧丢精度，也让 emp 与 app_key 两种形态在读取侧共用同一条解析路径。
func formatEmpID(empID int64) string {
	return strconv.FormatInt(empID, 10)
}
