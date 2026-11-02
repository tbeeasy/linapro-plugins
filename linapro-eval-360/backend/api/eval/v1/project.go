// Package v1 定义 360 测评管理端「项目与发送编排」API 的请求/响应 DTO。
package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ListProjectReq 列出当前租户下的全部项目。
type ListProjectReq struct {
	g.Meta `path:"/project" method:"get" tags:"Eval360 Project" summary:"项目列表" dc:"列出当前租户下的全部项目（管理员可见本租户全部）。" permission:"eval:360:list"`
}

// ListProjectRes 是项目列表响应。
type ListProjectRes struct {
	List []*ProjectListItem `json:"list" dc:"项目列表" eg:"[]"`
}

// ProjectListItem 是项目列表项。
type ProjectListItem struct {
	Id          int64  `json:"id" dc:"项目 id" eg:"1"`
	Name        string `json:"name" dc:"项目名称" eg:"2026 年度管理层 360 评估"`
	Description string `json:"description" dc:"项目描述" eg:"面向部门负责人的 360 度评估"`
	Status      string `json:"status" dc:"派生状态：draft=草稿|active=进行中|closed=已结束" eg:"active"`
	StartAt     *int64 `json:"startAt" dc:"评估开始时间（Unix timestamp in milliseconds）" eg:"1717200000000"`
	EndAt       *int64 `json:"endAt" dc:"项目统一截止时间（Unix timestamp in milliseconds）" eg:"1719792000000"`
	ClosedAt    *int64 `json:"closedAt" dc:"提前结束时间（Unix timestamp in milliseconds），未提前结束为 null" eg:"1717800000000"`
}

// GetProjectReq 取项目详情。
type GetProjectReq struct {
	g.Meta `path:"/project/{id}" method:"get" tags:"Eval360 Project" summary:"项目详情" dc:"项目详情（问卷卡片 + 跨问卷总完成率）。" permission:"eval:360:list"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id" eg:"1"`
}

// GetProjectRes 是项目详情响应。
type GetProjectRes struct {
	Detail *ProjectDetail `json:"detail" dc:"项目详情" eg:"{...}"`
}

// ProjectDetail 是项目详情。
type ProjectDetail struct {
	ProjectListItem
	Questionnaires []*QuestionnaireCard `json:"questionnaires" dc:"项目下问卷卡片列表" eg:"[]"`
	TotalCompleted int                  `json:"totalCompleted" dc:"跨问卷已填评价关系数" eg:"12"`
	TotalExpected  int                  `json:"totalExpected" dc:"跨问卷应填评价关系数" eg:"30"`
}

// QuestionnaireCard 是项目详情下的问卷卡片。
type QuestionnaireCard struct {
	Id             int64  `json:"id" dc:"问卷 id" eg:"10"`
	Title          string `json:"title" dc:"问卷标题" eg:"领导力评估"`
	Published      int    `json:"published" dc:"发布状态：0=未发布|1=已发布" eg:"1"`
	Sort           int    `json:"sort" dc:"排序" eg:"1"`
	EvaluateeCount int    `json:"evaluateeCount" dc:"被评估人数" eg:"5"`
	Completed      int    `json:"completed" dc:"已填评价关系数" eg:"3"`
	Expected       int    `json:"expected" dc:"应填评价关系数" eg:"8"`
	CreatedAt      *int64 `json:"createdAt" dc:"创建时间（Unix timestamp in milliseconds）" eg:"1717200000000"`
}

// CreateProjectReq 新建项目。
type CreateProjectReq struct {
	g.Meta      `path:"/project" method:"post" tags:"Eval360 Project" summary:"新建项目" dc:"新建一次评估活动。" permission:"eval:360:project:manage"`
	Name        string `json:"name" v:"required#请填写项目名称" dc:"项目名称"`
	Description string `json:"description" dc:"项目描述"`
	StartAt     *int64 `json:"startAt" dc:"评估开始时间（Unix timestamp in milliseconds）"`
	EndAt       *int64 `json:"endAt" dc:"项目统一截止时间（Unix timestamp in milliseconds）"`
}

// CreateProjectRes 返回新项目 id。
type CreateProjectRes struct {
	Id int64 `json:"id" dc:"新项目 id"`
}

// UpdateProjectReq 就地修改项目元信息。
type UpdateProjectReq struct {
	g.Meta      `path:"/project/{id}" method:"put" tags:"Eval360 Project" summary:"编辑项目" dc:"修改项目名称/描述/起止时间。" permission:"eval:360:project:manage"`
	Id          int64   `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
	Name        *string `json:"name" dc:"项目名称"`
	Description *string `json:"description" dc:"项目描述"`
	StartAt     *int64  `json:"startAt" dc:"评估开始时间（Unix timestamp in milliseconds）"`
	EndAt       *int64  `json:"endAt" dc:"项目统一截止时间（Unix timestamp in milliseconds）"`
}

// UpdateProjectRes 是空响应。
type UpdateProjectRes struct{}

// DeleteProjectReq 删除项目。
type DeleteProjectReq struct {
	g.Meta `path:"/project/{id}" method:"delete" tags:"Eval360 Project" summary:"删除项目" dc:"删除项目（存在已发布问卷或作答时拒绝）。" permission:"eval:360:project:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// DeleteProjectRes 是空响应。
type DeleteProjectRes struct{}

// CloseProjectReq 提前结束项目。
type CloseProjectReq struct {
	g.Meta `path:"/project/{id}/close" method:"post" tags:"Eval360 Project" summary:"提前结束" dc:"置 closed_at，不改 end_at。" permission:"eval:360:project:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// CloseProjectRes 是空响应。
type CloseProjectRes struct{}

// ReopenProjectReq 重新开启已提前结束的项目。
type ReopenProjectReq struct {
	g.Meta `path:"/project/{id}/reopen" method:"post" tags:"Eval360 Project" summary:"重新开启" dc:"清空 closed_at（不改 end_at）。" permission:"eval:360:project:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// ReopenProjectRes 是空响应。
type ReopenProjectRes struct{}

// SendProjectReq 一键发送。
type SendProjectReq struct {
	g.Meta `path:"/project/{id}/send" method:"post" tags:"Eval360 Project" summary:"一键发送" dc:"一键发送：整批校验项目下全部未发布问卷（项目内不得有空问卷、题目非空、角色权重合规、推送配置齐备，任一不满足即拒绝整个请求、不推送不发布），再向其评价者推送飞书卡片并按问卷判定发布：某份问卷的待评评价者全部送达才置为已发布，有评价者不可达或未送达的问卷保持未发布并在 failed 中逐份列出，不影响其他问卷（已送达的卡片不撤回）。部分或全部问卷推送失败仍正常返回；lark-push 未就绪或配置非法等请求级错误返回错误、不发布任何问卷。已发布跳过、幂等。" permission:"eval:360:publish"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// SendProjectRes 是一键发送结果。
type SendProjectRes struct {
	PublishedIDs []int64           `json:"publishedIds" dc:"本次新发布的问卷 id（已发布跳过、幂等；全部问卷推送失败时为空数组）" eg:"[10,11]"`
	Failed       []*SendFailedItem `json:"failed" dc:"Questionnaires left unpublished because card delivery did not pass, in pending-publish order; empty array when none failed" eg:"[{\"questionnaireId\":12,\"title\":\"Leadership 360 Survey\",\"reason\":\"unreachable\",\"employeeNos\":[\"GZ000071\"]}]"`
}

// SendFailedItem 是一键发送中一份因推送失败而未发布的问卷。
type SendFailedItem struct {
	QuestionnaireId int64    `json:"questionnaireId" dc:"ID of the questionnaire that was not published" eg:"12"`
	Title           string   `json:"title" dc:"Questionnaire title" eg:"Leadership 360 Survey"`
	Reason          string   `json:"reason" dc:"Why the questionnaire was not published: unreachable=the dry run found evaluators of this questionnaire who cannot receive the Lark card in any enterprise (evaluators who belong only to this questionnaire got no card); push_failed=after delivery some evaluators of this questionnaire were still not reached" eg:"unreachable"`
	EmployeeNos     []string `json:"employeeNos" dc:"Employee numbers of the failed evaluators in this questionnaire (the employee EmpID when an evaluator has no employee number)" eg:"[\"GZ000071\"]"`
}
