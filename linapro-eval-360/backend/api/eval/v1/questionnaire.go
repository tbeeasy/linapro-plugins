// Package v1 定义 360 测评管理端「问卷」相关 API 的请求/响应 DTO。
package v1

import (
	"github.com/gogf/gf/v2/frame/g"

	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// ListQuestionnaireReq 列出某项目下的问卷。
type ListQuestionnaireReq struct {
	g.Meta    `path:"/questionnaire" method:"get" tags:"Eval360 Questionnaire" summary:"问卷列表" dc:"列出某项目下的问卷（不含结构树）。" permission:"eval:360:query"`
	ProjectId int64 `json:"projectId" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// ListQuestionnaireRes 是问卷列表响应。
type ListQuestionnaireRes struct {
	List []*qsvc.QuestionnaireListItem `json:"list" dc:"问卷列表" eg:"[]"`
}

// PageQuestionnaireReq 分页列出某项目下的问卷（含完成率统计）。
type PageQuestionnaireReq struct {
	g.Meta    `path:"/questionnaire/page" method:"get" tags:"Eval360 Questionnaire" summary:"问卷分页" dc:"分页列出某项目下的问卷卡片（含被评估人数/完成率/创建时间），支持按标题搜索。" permission:"eval:360:query"`
	ProjectId int64  `json:"projectId" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
	Keyword   string `json:"keyword" dc:"按问卷标题模糊搜索（可空）"`
	PageNum   int    `json:"pageNum" d:"1" dc:"页码（从 1 起）"`
	PageSize  int    `json:"pageSize" d:"10" dc:"每页数量"`
}

// PageQuestionnaireRes 是问卷分页响应（字段名 items/total 匹配前端表格代理约定）。
type PageQuestionnaireRes struct {
	Items []*QuestionnaireCard `json:"items" dc:"问卷卡片列表" eg:"[]"`
	Total int                  `json:"total" dc:"总数" eg:"23"`
}

// GetQuestionnaireReq 取问卷完整结构树。
type GetQuestionnaireReq struct {
	g.Meta `path:"/questionnaire/{id}" method:"get" tags:"Eval360 Questionnaire" summary:"问卷详情" dc:"返回问卷完整 live 结构树（维度/题目/选项/矩阵/可见性 + 角色）。" permission:"eval:360:query"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// GetQuestionnaireRes 是问卷详情响应。
type GetQuestionnaireRes struct {
	Detail *qsvc.QuestionnaireDetail `json:"detail" dc:"问卷结构树" eg:"{...}"`
}

// CreateQuestionnaireReq 新建问卷（默认未发布）。
type CreateQuestionnaireReq struct {
	g.Meta      `path:"/questionnaire" method:"post" tags:"Eval360 Questionnaire" summary:"新建问卷" dc:"在项目下新建一份未发布问卷。" permission:"eval:360:add"`
	ProjectId   int64  `json:"projectId" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
	Title       string `json:"title" v:"required#请填写问卷标题" dc:"问卷标题"`
	Description string `json:"description" dc:"问卷描述"`
	Sort        int    `json:"sort" dc:"排序"`
}

// CreateQuestionnaireRes 返回新问卷 id。
type CreateQuestionnaireRes struct {
	Id int64 `json:"id" dc:"新问卷 id"`
}

// UpdateQuestionnaireReq 就地修改问卷元信息。
type UpdateQuestionnaireReq struct {
	g.Meta      `path:"/questionnaire/{id}" method:"put" tags:"Eval360 Questionnaire" summary:"编辑问卷" dc:"就地修改问卷标题/描述/排序（草稿/收集期间均可）。" permission:"eval:360:edit"`
	Id          int64   `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
	Title       *string `json:"title" dc:"问卷标题"`
	Description *string `json:"description" dc:"问卷描述"`
	Sort        *int    `json:"sort" dc:"排序"`
}

// UpdateQuestionnaireRes 是空响应。
type UpdateQuestionnaireRes struct{}

// DeleteQuestionnaireReq 删除问卷。
type DeleteQuestionnaireReq struct {
	g.Meta `path:"/questionnaire/{id}" method:"delete" tags:"Eval360 Questionnaire" summary:"删除问卷" dc:"删除问卷及其结构（存在作答时拒绝）。" permission:"eval:360:remove"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// DeleteQuestionnaireRes 是空响应。
type DeleteQuestionnaireRes struct{}
