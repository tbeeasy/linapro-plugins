// export.go 定义 360 测评管理端「明细导出」API 的请求/响应 DTO。
// 仅挂管理端 Auth()/Permission()，位于问卷详情「评估报告」菜单；无员工端入口。
package v1

import "github.com/gogf/gf/v2/frame/g"

// ExportByAnswerReq 按答案导出（全部题原文，评语并入单元格【】包裹）。
type ExportByAnswerReq struct {
	g.Meta          `path:"/export/by-answer" method:"get" tags:"Eval360 Export" summary:"按答案导出" dc:"按答案导出明细 Excel（只导已提交）。" permission:"eval:360:export"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ExportByAnswerRes 是空响应（走文件流）。
type ExportByAnswerRes struct{}

// ExportByScoreReq 按得分导出（仅计分题，分值为数字单元格，0 分即无法评价与未作答均留空）。
type ExportByScoreReq struct {
	g.Meta          `path:"/export/by-score" method:"get" tags:"Eval360 Export" summary:"按得分导出" dc:"按得分导出明细 Excel（只导已提交）。" permission:"eval:360:export"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ExportByScoreRes 是空响应（走文件流）。
type ExportByScoreRes struct{}
