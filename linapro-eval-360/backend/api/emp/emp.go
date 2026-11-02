// Package emp 定义 360 测评「员工端」控制器接口，经 group.Bind() 绑定。
// 员工端路由挂 empcap.EmpAuth()（复用 linapro-employee-core 的员工认证），
// 不挂宿主 Auth()/Permission()。
package emp

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/emp/v1"
)

// IEmpV1 是员工端 v1 控制器接口。
type IEmpV1 interface {
	// Whoami 回显 EmpAuth 注入的员工身份，验证员工端认证链已就绪。
	Whoami(ctx context.Context, req *v1.WhoamiReq) (res *v1.WhoamiRes, err error)
	// Todo 我的待办（按问卷聚合，一份问卷一条）
	Todo(ctx context.Context, req *v1.TodoReq) (res *v1.TodoRes, err error)
	// Questions 按问卷取批量题集（被评估人行 + 逐行可见题目 + 已提交答案）
	Questions(ctx context.Context, req *v1.QuestionsReq) (res *v1.QuestionsRes, err error)
	// Submit 批量提交作答（单事务全成全败，按被评估人各自落库）
	Submit(ctx context.Context, req *v1.SubmitReq) (res *v1.SubmitRes, err error)
}
