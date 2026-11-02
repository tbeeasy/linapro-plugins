// publish.go 定义 360 测评管理端「发布 / 编辑策略」API 的请求/响应 DTO。
package v1

import "github.com/gogf/gf/v2/frame/g"

// PublishQuestionnaireReq 发布单份问卷。
type PublishQuestionnaireReq struct {
	g.Meta `path:"/questionnaire/{id}/publish" method:"post" tags:"Eval360 Publish" summary:"发布问卷" dc:"发布单份问卷：校验题目非空、角色权重与推送配置后向其评价者推送飞书卡片，全部评价者送达才置为已发布；任一评价者不可达或未送达即返回错误、问卷保持未发布（已送达的卡片不撤回）。已发布跳过、不重推。" permission:"eval:360:publish"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// PublishQuestionnaireRes 是发布单份问卷结果。
type PublishQuestionnaireRes struct {
	PublishedIDs []int64 `json:"publishedIds" dc:"本次新发布的问卷 id（已发布跳过时为空）" eg:"[10]"`
}

// UnpublishQuestionnaireReq 取消发布单份问卷。
type UnpublishQuestionnaireReq struct {
	g.Meta `path:"/questionnaire/{id}/unpublish" method:"post" tags:"Eval360 Publish" summary:"取消发布问卷" dc:"取消发布单份问卷。" permission:"eval:360:publish"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// UnpublishQuestionnaireRes 是空响应。
type UnpublishQuestionnaireRes struct{}

// PublishProjectReq 发布项目下全部未发布问卷（已发布跳过、幂等）。
type PublishProjectReq struct {
	g.Meta    `path:"/project/{projectId}/publish-unpublished" method:"post" tags:"Eval360 Publish" summary:"发布项目全部未发布问卷" dc:"发布项目下全部未发布问卷（已发布跳过、幂等），与一键发送同一编排：整批校验，任一不满足即拒绝整个请求；推送后按问卷判定，评价者全部收到飞书卡片的问卷才置为已发布，其余问卷保持未发布并在 failed 中逐份列出，部分或全部失败仍正常返回；请求级推送错误返回错误、不发布任何问卷。" permission:"eval:360:publish"`
	ProjectId int64 `json:"projectId" in:"path" v:"required|min:1#缺少项目 id|项目 id 非法" dc:"项目 id"`
}

// PublishProjectRes 返回本次发布的问卷 id 与未发布问卷清单。
type PublishProjectRes struct {
	PublishedIds []int64           `json:"publishedIds" dc:"本次发布的问卷 id（已发布跳过、幂等；全部问卷推送失败时为空数组）" eg:"[10,11]"`
	Failed       []*SendFailedItem `json:"failed" dc:"Questionnaires left unpublished because card delivery did not pass, in pending-publish order; empty array when none failed. Same structure as one-click send" eg:"[{\"questionnaireId\":12,\"title\":\"Leadership 360 Survey\",\"reason\":\"push_failed\",\"employeeNos\":[\"GZ000071\"]}]"`
}

// CopyQuestionnaireReq 复制问卷（策略③）。
type CopyQuestionnaireReq struct {
	g.Meta `path:"/questionnaire/{id}/copy" method:"post" tags:"Eval360 Publish" summary:"复制问卷" dc:"深拷贝结构 + 被评估人→评价关系树为未发布新问卷（不复制答案）。" permission:"eval:360:add"`
	Id     int64  `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"源问卷 id"`
	Title  string `json:"title" v:"required#请填写新问卷名称" dc:"新问卷名称（同项目内不可与其他问卷重名；不沿用源标题）"`
}

// CopyQuestionnaireRes 返回新问卷 id。
type CopyQuestionnaireRes struct {
	Id int64 `json:"id" dc:"复制生成的新问卷 id"`
}

// ClearAnswersReq 清空问卷全部作答（策略②）。
type ClearAnswersReq struct {
	g.Meta `path:"/questionnaire/{id}/clear-answers" method:"post" tags:"Eval360 Publish" summary:"清空作答" dc:"清空问卷全部作答并重置相关评价关系完成态。" permission:"eval:360:edit"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ClearAnswersRes 是空响应。
type ClearAnswersRes struct{}
