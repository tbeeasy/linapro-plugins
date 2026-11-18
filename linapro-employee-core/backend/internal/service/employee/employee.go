// Package employee 实现核心的员工写入网关与读取查询。
// 它是员工表和 lark_identity 映射表的唯一写入方，强制 EmpID 分配、
// (tenant_id, moka_employee_no) 去重键以及在职状态机（empcap.Service）。
//
// 文件组织：本文件只保留组件契约（组件说明、Service 结构体、编译期断言与构造函数），
// 具体实现按职责拆分在同包文件中——employee_write.go 写入网关、employee_query.go
// 单条读、employee_batch.go 批量读与读模型装配、employee_lark_identity.go 飞书身份
// 解析与批量寻址、employee_admin.go 后台花名册读写。
package employee

import (
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// Service 是 empcap.Service 的拥有方插件实现。
//
// 它不持有运行期依赖：员工表读写全部经由包级 dao，租户隔离由调用方传入的
// tenantID 决定，权限判定不属于 empcap 的职责（契约只答「是谁」，不答「能看什么」）。
type Service struct{}

// 编译期断言：Service 满足共享契约。
var _ empcap.Service = (*Service)(nil)

// New 返回新的员工服务。
//
// 服务无状态，可安全地在启动装配期构造一次后共享（plugin.go 的 sharedEmployee）；
// 每次调用返回新实例也不会引入不一致。
func New() *Service { return &Service{} }
