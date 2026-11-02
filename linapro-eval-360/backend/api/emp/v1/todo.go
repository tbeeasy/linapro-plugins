// todo.go 定义员工端「我的待办」接口 DTO：按问卷聚合，一个评价者 + 一份问卷 = 一条待办。

package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// TodoReq 我的待办列表。
type TodoReq struct {
	g.Meta `path:"/todo" method:"get" tags:"Eval360 Emp" summary:"我的待办" dc:"按问卷聚合返回当前员工已发布问卷的待办及完成度（已完成 X/Y 人）；条数受当前员工参与的已发布问卷数约束，无分页。"`
}

// TodoRes 是待办列表响应。
type TodoRes struct {
	List []*TodoItem `json:"list" dc:"待办列表（一份问卷一条）" eg:"[]"`
}

// TodoItem 是待办列表项（一份问卷一条）。
type TodoItem struct {
	QuestionnaireID    int64            `json:"questionnaireId" dc:"问卷 id" eg:"10"`
	ProjectID          int64            `json:"projectId" dc:"项目 id" eg:"1"`
	ProjectName        string           `json:"projectName" dc:"项目名称" eg:"2026 年度管理层 360 评估"`
	QuestionnaireTitle string           `json:"questionnaireTitle" dc:"问卷标题" eg:"领导力评估"`
	StartAt            *int64           `json:"startAt" dc:"项目开始时间（Unix timestamp in milliseconds），null=未设置" eg:"1717200000000"`
	EndAt              *int64           `json:"endAt" dc:"截止时间（Unix timestamp in milliseconds），null=无截止" eg:"1719792000000"`
	Total              int              `json:"total" dc:"名下被评估人总数" eg:"3"`
	CompletedCount     int              `json:"completedCount" dc:"已提交的被评估人数" eg:"1"`
	DerivedStatus      string           `json:"derivedStatus" dc:"派生状态：pending=待填写|filled=全部已提交|voided=未填完且已截止" eg:"pending"`
	Evaluatees         []*TodoEvaluatee `json:"evaluatees" dc:"名下被评估人明细（按关系 id 升序）" eg:"[]"`
}

// TodoEvaluatee 是待办下的一个被评估人（对应一条评价关系）。
type TodoEvaluatee struct {
	RelationID    int64  `json:"relationId" dc:"评价关系 id" eg:"100"`
	EvaluateeName string `json:"evaluateeName" dc:"被评估人姓名" eg:"张三"`
	JobTitle      string `json:"jobTitle" dc:"被评估人职务（可空）" eg:"主美"`
	Department    string `json:"department" dc:"被评估人部门（可空）" eg:"美术部"`
	RoleName      string `json:"roleName" dc:"评价关系角色" eg:"同事"`
	Submitted     bool   `json:"submitted" dc:"是否已提交" eg:"false"`
}
