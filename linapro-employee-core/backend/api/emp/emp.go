// Package emp 定义员工登录与身份控制器契约。
// Login 是公开的（飞书 code 换票）；Me 由 EmpAuth 保护。它们是两个
// 独立的控制器，以便各自绑定到具有正确中间件的路由组 ——
// GoFrame 会把一个控制器的所有方法绑定在一起。
package emp

import (
	"context"

	v1 "lina-plugin-linapro-employee-core/backend/api/emp/v1"
)

// ILoginV1 是公开的员工登录 API。
type ILoginV1 interface {
	Login(ctx context.Context, req *v1.LoginReq) (res *v1.LoginRes, err error)
}

// IMeV1 是受 EmpAuth 保护的员工身份 API。
type IMeV1 interface {
	Me(ctx context.Context, req *v1.MeReq) (res *v1.MeRes, err error)
}
