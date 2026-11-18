// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Apilog is the golang structure of table plugin_linapro_monitor_apilog for DAO operations like Where/Data.
type Apilog struct {
	g.Meta       `orm:"table:plugin_linapro_monitor_apilog, do:true"`
	Id           any        // 记录 ID，由序列生成（分区表不支持 identity 列）
	RequestAt    *time.Time // 分区键：请求开始时间，与 cost_ms 同一基准；列表筛选、排序与详情定位都用它
	RequestId    any        // 请求 ID，与业务日志互查
	RouteOwner   any        // 路由归属插件 ID，用于按插件筛选与排除名单匹配
	Method       any        // HTTP 方法
	Path         any        // 实际请求路径
	RoutePath    any        // 路由模式，例如 /x/.../task/{id}；动态插件取真实路径
	Query        any        // 原始查询串，敏感参数已脱敏
	ClientIp     any        // 客户端 IP
	UserAgent    any        // User-Agent
	CallerKind   any        // 调用方类型：emp=员工 app_key=外部系统
	CallerId     any        // 调用方标识：emp 时为 EmpID，app_key 时为 app_key；唯一的机器关联键
	CallerTenant any        // 调用方租户 ID，无租户语义时为 0
	CallerName   any        // 调用方姓名快照，只用于显示与按人搜索
	CallerNo     any        // 调用方工号快照，只用于显示与按人搜索
	StatusCode   any        // HTTP 状态码
	CostMs       any        // 耗时，单位毫秒
	ReqHeaders   any        // 请求头 JSON，已脱敏
	ReqBody      any        // 完整请求体，已脱敏
	RespBody     any        // 完整响应体，已脱敏
	ErrorMsg     any        // 错误信息
	CreatedAt    *time.Time // 写库时间，由框架自动写入
}
