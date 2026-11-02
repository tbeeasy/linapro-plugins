// Package emp 将员工登录（公开）与身份（受保护）控制器实现为两个类型，
// 以便各自绑定在正确的中间件之下。
//
// emp_new.go 定义两个控制器及其构造函数。MeController 依赖插件拥有的
// empcap.Service 宽入口（而非 internal/service 的具体类型），使身份读取
// 与已发布的契约保持一致；LoginController 只依赖登录服务，因为登录结果
// 已携带身份快照，无需再持有员工目录入口。
package emp

import (
	empapi "lina-plugin-linapro-employee-core/backend/api/emp"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/service/login"
)

// LoginController 处理公开的飞书登录换票。
type LoginController struct {
	logins *login.Service
}

// NewLoginV1 创建公开的登录控制器。
func NewLoginV1(logins *login.Service) empapi.ILoginV1 {
	return &LoginController{logins: logins}
}

// MeController 处理受 EmpAuth 保护的当前员工查询。
type MeController struct {
	employees empcap.Service
}

// NewMeV1 创建受保护的身份控制器。
func NewMeV1(employees empcap.Service) empapi.IMeV1 {
	return &MeController{employees: employees}
}
