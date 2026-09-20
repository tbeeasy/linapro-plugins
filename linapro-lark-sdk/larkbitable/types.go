// Package larkbitable 封装飞书（Lark）多维表格（Bitable）OpenAPI 的二次调用：
// REST 读写骨架（分页、批量切块、限流）与字段值编解码，作为共享库供多个插件复用。
// 一份 app 级凭证在所有数据表间共享，每张表由自己的 AppToken + TableID 定位。
package larkbitable

import (
	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// 人员字段可用的用户 id 类型识别符（写入请求的 user_id_type 参数）。
// open_id 应用内唯一、不可跨应用交叉；跨应用写入（如目录来源应用与目标 Bitable 应用
// 不同）须使用 user_id。调用方经 CreateOp.Persons / UpdateOp.Persons 旁路传入的 id 类型
// 必须与 Encoder.UserIDType 保持一致。
const (
	// UserIDTypeOpenID 以 open_id 识别用户（默认）。
	UserIDTypeOpenID = larkbitablesdk.UserIdTypeOpenId
	// UserIDTypeUserID 以 user_id 识别用户（企业级，跨该企业所有应用一致）。
	UserIDTypeUserID = larkbitablesdk.UserIdTypeUserId
	// UserIDTypeUnionID 以 union_id 识别用户。
	UserIDTypeUnionID = larkbitablesdk.UserIdTypeUnionId
)

// Table 定位一张 Bitable 数据表。
type Table struct {
	AppToken string
	TableID  string
}

// Row 是读路径的「字段名 → 文本值」投影：ListRecords/ListRecordsByID/BatchGetByIDs 回读记录时，
// 各类型单元格（数字/日期/富文本段数组/人员对象）统一经 cellToString 规范化为字符串
// （见 cellToString），故读侧值恒为 string，消费方直接取用、无需类型断言。
// 写路径用 WriteRow（值为强类型）——读永远是文本、写才需要类型，二者刻意分开。
type Row map[string]string

// WriteRow 是写路径的「字段名 → 强类型值」映射，对齐飞书 Fields 的真实形态
// （map[string]interface{}）。写入方在自身边界把日期解析为 int64 毫秒、数字解析为 float64、
// 文本保持 string 后直接放入，共享库只做序列化、不再持有任何日期/数字解析逻辑（见 rowToFields）。
// 人员/附件列不走 WriteRow，各自经 Persons/Attachments 旁路。
type WriteRow map[string]any

// ExistingRecord 表示 ListRecords 返回的一条记录，以调用方指定的 uniqueField 值为键。
//
// Fields 是全列文本投影（读 Row，值恒为 string）：人员列存去重姓名串（供默认文本比对），
// 其余列存文本。
// Persons 是人员列的结构化旁路：键为列名，值为飞书官方 []*larkbitablesdk.Person
// （每个含 id/name/en_name/email/avatar_url）。需要 open_id、email 等非姓名信息，
// 或想做 id 漂移比对（同名换人、离职复入导致 open_id 变更）的消费方读 Persons；
// 只做文本比对的消费方读 Fields 即可。没有人员列时 Persons 为 nil。
type ExistingRecord struct {
	RecordID string
	Fields   Row
	Persons  map[string][]*larkbitablesdk.Person
}

// CreateOp 表示一条待新建的记录。
type CreateOp struct {
	Fields WriteRow
	// Attachments 将附件列名映射到要放入该列的飞书 file_token 列表；token 由
	// UploadMedia 获得。记录无附件列时为 nil。
	Attachments map[string][]string
	// Persons 将人员列名映射到要放入该列的飞书用户 id 列表（列名 → open_id 集合），
	// 由调用方在自身边界解析完毕（如经 empcap 把工号解析为 open_id）后传入，共享库只做
	// 序列化、不做任何身份解析。id 类型须与 Encoder.UserIDType 一致，默认 open_id。
	// 记录无人员列时为 nil；某列 id 集合为空时该列被跳过（不写入）。
	Persons map[string][]string
}

// UpdateOp 表示一条按 record_id 更新的记录。
type UpdateOp struct {
	RecordID string
	Fields   WriteRow
	// Attachments 语义同 CreateOp.Attachments。
	Attachments map[string][]string
	// Persons 语义同 CreateOp.Persons。
	Persons map[string][]string
}
