// submit.go 定义员工端「批量提交作答」接口 DTO：按被评估人（评价关系）分组提交，单事务全成全败。

package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// SubmitReq 批量提交作答。
type SubmitReq struct {
	g.Meta          `path:"/submit" method:"post" tags:"Eval360 Emp" summary:"批量提交作答" dc:"提交页面上已展示的未提交被评估人，单事务全成全败；请求内全部已提交按幂等返回，部分已提交整体拒绝。items 只能包含当前员工在该问卷下的评价关系，超出即拒绝，故数量受其关系数约束。"`
	QuestionnaireId int64           `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id" eg:"10"`
	DurationSeconds int             `json:"durationSeconds" dc:"作答时长（前端整场计时上传，整数秒；每条提交记录存同一值）" eg:"120"`
	Items           []SubmitItemDTO `json:"items" v:"required#缺少提交内容" dc:"按被评估人（评价关系）分组的答案；不可为空、不可重复" eg:"[]"`
}

// SubmitItemDTO 是单个被评估人（评价关系）的答案集合。
type SubmitItemDTO struct {
	RelationId int64       `json:"relationId" dc:"评价关系 id" eg:"21"`
	Answers    []AnswerDTO `json:"answers" dc:"答案列表（一题一条）" eg:"[]"`
}

// AnswerDTO 是单条答案入参。
type AnswerDTO struct {
	QuestionId int64  `json:"questionId" dc:"题目 id" eg:"101"`
	OptionId   int64  `json:"optionId" dc:"单选选项 id / 矩阵列 id / 文本为 0" eg:"201"`
	TextValue  string `json:"textValue" dc:"文本答案 或 选项级评语" eg:""`
}

// SubmitRes 是批量提交结果。
type SubmitRes struct {
	SubmittedCount int    `json:"submittedCount" dc:"本次提交（或幂等命中）的被评估人数" eg:"2"`
	SubmittedAt    *int64 `json:"submittedAt" dc:"提交时间（Unix timestamp in milliseconds）" eg:"1717800000000"`
	Idempotent     bool   `json:"idempotent" dc:"是否重复提交返回首次结果" eg:"false"`
}
