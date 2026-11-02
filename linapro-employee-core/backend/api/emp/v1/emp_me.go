// emp_me.go 定义受保护的当前员工身份 DTO。Me 需要员工 JWT，
// 并从 EmpAuth 注入的 EmpContext 中读取身份。
package v1

import "github.com/gogf/gf/v2/frame/g"

// MeReq 返回当前员工身份。需要员工 JWT。
type MeReq struct {
	g.Meta `path:"/emp/me" method:"get" tags:"Attendance / Employee" summary:"Current employee" dc:"返回由员工 JWT 解析出的当前员工身份，并进行一次实时状态校验。"`
}

// MeRes 是当前员工身份快照。
type MeRes struct {
	EmpId          int64    `json:"empId" dc:"内部员工 id" eg:"1001"`
	TenantId       int      `json:"tenantId" dc:"租户 id" eg:"1"`
	Name           string   `json:"name" dc:"员工显示名称" eg:"Jane Doe"`
	MokaEmployeeNo string   `json:"mokaEmployeeNo" dc:"Moka HR 系统工号（如 GZ001198）；未设置时为空" eg:"GZ001198"`
	Status         int      `json:"status" dc:"1=在职 0=离职" eg:"1"`
	DepartmentIds  []string `json:"departmentIds" dc:"员工所属的飞书原始部门 id（非 org-core 部门 id）；部门数据当前预留、尚未写入，暂恒为空数组" eg:"[\"od-1a2b3c\"]"`
}
