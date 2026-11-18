// middleware_caller.go 解析调用方标识。
//
// 约定（见 design D6）：鉴权中间件通过后把一段 JSON 写进请求上下文变量
// `linapro.request.caller`，形如 {"kind":"emp","id":"5","tenant":1,"name":"张三","no":"GZ001198"}。
// 日志插件只读这个约定键，因此与业务插件之间零依赖：日志插件未安装时，这个变量没有任何读者。
//
// 拆分存储：没有写入时各列为空、记录照常写入；任一字段缺失只影响对应列；
// 解析失败时各列留空并记一条带 request_id 的 WARNING，绝不丢弃整条记录。

package middleware

import (
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"

	"lina-core/pkg/logger"
)

// callerCtxKey 是与写入方约定的上下文变量键名。
//
// 写入方各自持有同名常量副本（它们不能 import 本插件），单测锁值防止漂移。
const callerCtxKey = "linapro.request.caller"

// callerFields 是调用方约定的 JSON 结构。
type callerFields struct {
	// Kind 是调用方类型：emp（员工）或 app_key（外部系统）。
	Kind string `json:"kind"`
	// ID 是 emp 时的 EmpID，app_key 时的 app_key；唯一的机器关联键。
	ID string `json:"id"`
	// Tenant 是租户 ID，无租户语义时省略（解析后为 0）。
	Tenant int `json:"tenant"`
	// Name 是员工姓名快照，拿不到时省略。
	Name string `json:"name"`
	// No 是 Moka 工号快照，拿不到时省略。
	No string `json:"no"`
}

// parseCaller 读取并解析调用方约定键。未写入时返回零值，记录照常写入。
func parseCaller(request *ghttp.Request) callerFields {
	raw := strings.TrimSpace(request.GetCtxVar(callerCtxKey).String())
	if raw == "" {
		return callerFields{}
	}
	var caller callerFields
	if err := json.Unmarshal([]byte(raw), &caller); err != nil {
		// 解析失败不丢记录：其余列（路径、状态、请求体）仍是排查依据。
		logger.Warningf(request.Context(), "linapro-monitor-apilog：解析调用方标识失败，本次记录 caller_* 留空 request_id=%s: %v",
			gctx.CtxId(request.Context()), err)
		return callerFields{}
	}
	return caller
}
