// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"

	"github.com/shopspring/decimal"
)

// Role is the golang structure for table role.
type Role struct {
	Id              int64            `json:"id"              orm:"id"               description:""`
	QuestionnaireId int64            `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	Name            string           `json:"name"            orm:"name"             description:"角色名（归一化后精确匹配）"`
	Weight          *decimal.Decimal `json:"weight"          orm:"weight"           description:"角色权重，NUMERIC；发布前每角色 >0 且和=100%"`
	CreatedAt       *time.Time       `json:"createdAt"       orm:"created_at"       description:""`
	UpdatedAt       *time.Time       `json:"updatedAt"       orm:"updated_at"       description:""`
}
