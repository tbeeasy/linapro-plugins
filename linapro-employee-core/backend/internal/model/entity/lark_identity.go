// =================================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// =================================================================================

package entity

import (
	"time"
)

// LarkIdentity 是表 lark_identity 的 Go 结构体。
type LarkIdentity struct {
	LarkAppId  string     `json:"larkAppId"  orm:"lark_app_id"  description:"飞书 App ID（cli_xxx）；标识该 open_id 所属的飞书企业"`
	LarkOpenId string     `json:"larkOpenId" orm:"lark_open_id" description:"飞书应用内 open_id；同一 lark_app_id 下唯一"`
	LarkUserId string     `json:"larkUserId" orm:"lark_user_id" description:"飞书 user_id（企业级，跨该企业所有 app 一致）；考勤等按 user_id 推送的数据源用它反查 emp_id；应用无 user_id 权限时为空"`
	IdSource   string     `json:"idSource"   orm:"id_source"    description:"身份来源：own=本租户原生身份（可登录/考勤反查）；assoc=关联组织视角身份（仅供写多维表格人员字段，不参与登录/反查）"`
	EmpId      int64      `json:"empId"      orm:"emp_id"       description:"映射到的内部 EmpID；同一 emp_id 可有多条（跨飞书企业）"`
	CreatedAt  *time.Time `json:"createdAt"  orm:"created_at"   description:"创建时间"`
}
