// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Answer is the golang structure for table answer.
type Answer struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	SubmissionId    int64      `json:"submissionId"    orm:"submission_id"    description:"所属作答 id"`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	QuestionId      int64      `json:"questionId"      orm:"question_id"      description:"所属题目 id"`
	RowId           int64      `json:"rowId"           orm:"row_id"           description:"矩阵题行 id，非矩阵为 0"`
	OptionId        int64      `json:"optionId"        orm:"option_id"        description:"单选选项 id / 矩阵列 id / 文本为 0"`
	TextValue       string     `json:"textValue"       orm:"text_value"       description:"文本题答案 或 选项级评语（allow_text）"`
	CreatedAt       *time.Time `json:"createdAt"       orm:"created_at"       description:""`
}
