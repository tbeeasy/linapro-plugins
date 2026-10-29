// write.go 提供 Bitable 的批量写入（新建 / 覆盖更新），按 maxBatchSize 切批并节流；
// 写入前比对字段名与字段类型并记入诊断日志，便于定位 1254045 / 99992402 一类拒写。

package larkbitable

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"lina-core/pkg/logger"
	"linapro-lark-sdk/utils"
)

// userIDType 返回下发飞书的 user_id_type 查询参数值，空则兜底 open_id。
// 该参数仅在写入人员字段时用于指定用户 ID 的解析格式；未配置（空串）时返回 ok=false，
// 下发处应省略 UserIdType，避免空串触发飞书 99992402 field validation failed。
func (c *Client) userIDType() (string, bool) {
	if c.encoder.UserIDType == "" {
		return "", false
	}
	return c.encoder.UserIDType, true
}

// BatchCreate 按 maxBatchSize 切批新建记录，批次间节流。返回全部新建记录的飞书
// record_id，顺序与输入一致。
func (c *Client) BatchCreate(ctx context.Context, t Table, creates []CreateOp, fieldTypes map[string]int) ([]string, error) {
	rows := make([]WriteRow, len(creates))
	for i, op := range creates {
		rows[i] = op.Fields
	}
	logFieldMismatch(ctx, "batch create", rows, fieldTypes)

	var ids []string
	for start := 0; start < len(creates); start += maxBatchSize {
		end := min(start+maxBatchSize, len(creates))
		records := make([]*larkbitablesdk.AppTableRecord, 0, end-start)
		fieldsForLog := make([]map[string]any, 0, end-start)
		for _, op := range creates[start:end] {
			f := c.rowToFields(ctx, op.Fields, op.Attachments, op.Persons, fieldTypes)
			// 编码后字段全空（例如唯一的列是无法解析的人员列，被跳列）时，跳过该记录：
			// 飞书 batch_create 对 fields 为空的记录会整批判定为非法数据（99992402）。
			if len(f) == 0 {
				logger.Debug(ctx, "lark: 批量创建跳过记录：编码后无可写字段")
				continue
			}
			records = append(records, larkbitablesdk.NewAppTableRecordBuilder().Fields(f).Build())
			fieldsForLog = append(fieldsForLog, f)
		}
		if len(records) == 0 {
			continue
		}
		if raw, mErr := json.Marshal(fieldsForLog); mErr == nil {
			uidType, _ := c.userIDType()
			logger.Debugf(ctx, "lark: 批量创建请求体 user_id_type=%q: %s", uidType, string(raw))
		}
		builder := larkbitablesdk.NewBatchCreateAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).
			Body(larkbitablesdk.NewBatchCreateAppTableRecordReqBodyBuilder().
				Records(records).Build())
		// user_id_type 仅在写人员字段时需要；未配置则省略该查询参数（空串会被飞书判非法）。
		if uidType, ok := c.userIDType(); ok {
			builder = builder.UserIdType(uidType)
		}
		req := builder.Build()
		resp, err := c.client.Bitable.V1.AppTableRecord.BatchCreate(ctx, req)
		if err != nil {
			return nil, gerror.Wrap(err, "lark: 批量创建记录请求失败")
		}
		if !resp.Success() {
			return nil, gerror.Newf("lark: 批量创建记录失败 req_id=%s code=%d msg=%s%s; 写入字段=%v; 表字段=%v",
				resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError), writtenFieldTypes(rows, fieldTypes), sortedKeys(fieldTypes))
		}
		if resp.Data != nil {
			for _, rec := range resp.Data.Records {
				ids = append(ids, larkcore.StringValue(rec.RecordId))
			}
		}
		time.Sleep(throttle)
	}
	return ids, nil
}

// BatchUpdate 按 maxBatchSize 切批覆盖更新记录，批次间节流。
func (c *Client) BatchUpdate(ctx context.Context, t Table, updates []UpdateOp, fieldTypes map[string]int) error {
	rows := make([]WriteRow, len(updates))
	for i, op := range updates {
		rows[i] = op.Fields
	}
	logFieldMismatch(ctx, "batch update", rows, fieldTypes)

	for start := 0; start < len(updates); start += maxBatchSize {
		end := min(start+maxBatchSize, len(updates))
		records := make([]*larkbitablesdk.AppTableRecord, 0, end-start)
		fieldsForLog := make([]map[string]any, 0, end-start)
		for _, op := range updates[start:end] {
			f := c.rowToFields(ctx, op.Fields, op.Attachments, op.Persons, fieldTypes)
			// 编码后字段全空（例如唯一的差异列是无法解析的人员列，被跳列）时，跳过该记录：
			// 飞书 batch_update 对 fields 为空的记录会整批判定为非法数据（99992402）。
			if len(f) == 0 {
				logger.Debugf(ctx, "lark: 批量更新跳过记录 %s：编码后无可写字段", op.RecordID)
				continue
			}
			records = append(records, larkbitablesdk.NewAppTableRecordBuilder().
				RecordId(op.RecordID).Fields(f).Build())
			fieldsForLog = append(fieldsForLog, f)
		}
		if len(records) == 0 {
			continue
		}
		if raw, mErr := json.Marshal(fieldsForLog); mErr == nil {
			uidType, _ := c.userIDType()
			logger.Debugf(ctx, "lark: 批量更新请求体 user_id_type=%q: %s", uidType, string(raw))
		}
		builder := larkbitablesdk.NewBatchUpdateAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).
			Body(larkbitablesdk.NewBatchUpdateAppTableRecordReqBodyBuilder().
				Records(records).Build())
		// user_id_type 仅在写人员字段时需要；未配置则省略该查询参数（空串会被飞书判非法）。
		if uidType, ok := c.userIDType(); ok {
			builder = builder.UserIdType(uidType)
		}
		req := builder.Build()
		resp, err := c.client.Bitable.V1.AppTableRecord.BatchUpdate(ctx, req)
		if err != nil {
			return gerror.Wrap(err, "lark: 批量更新记录请求失败")
		}
		if !resp.Success() {
			return gerror.Newf("lark: 批量更新记录失败 req_id=%s code=%d msg=%s%s; 写入字段=%v; 表字段=%v;",
				resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError), writtenFieldTypes(rows, fieldTypes), sortedKeys(fieldTypes))
		}
		time.Sleep(throttle)
	}
	return nil
}

// logFieldMismatch 在写入前比对各行字段名与表实际字段集（fieldTypes 的键），把表中不存在
// 的字段名记入可诊断日志，便于定位 FieldNameNotFound（1254045）。
func logFieldMismatch(ctx context.Context, op string, rows []WriteRow, fieldTypes map[string]int) {
	tableFields := sortedKeys(fieldTypes)

	unknown := make(map[string]struct{})
	for _, row := range rows {
		for name := range row {
			if _, ok := fieldTypes[name]; !ok {
				unknown[name] = struct{}{}
			}
		}
	}

	if len(unknown) == 0 {
		// 字段名全部命中时，额外打出**写入字段的解析类型**（如 匹配度等级=MultiSelect(4)），
		// 便于定位 99992402（字段值校验失败）——编码器只特殊处理 Number/DateTime/User，
		// 其余类型一律按裸字符串写，若某列实为单选/多选/复选框/关联等就会被飞书拒绝。
		logger.Debugf(ctx, "lark: %s 字段校验通过；写入字段类型=%v；表字段=%v",
			op, writtenFieldTypes(rows, fieldTypes), tableFields)
		return
	}
	missing := make([]string, 0, len(unknown))
	for name := range unknown {
		missing = append(missing, name)
	}
	sort.Strings(missing)
	logger.Warningf(ctx, "lark: %s 字段不匹配——以下字段名在表中不存在：%v；实际表字段：%v", op, missing, tableFields)
}

// sortedKeys 返回字段类型映射按字典序排序的键。
func sortedKeys(fieldTypes map[string]int) []string {
	keys := make([]string, 0, len(fieldTypes))
	for name := range fieldTypes {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// distinctRowFields 返回各行出现过的去重字段名（按字典序）。
func distinctRowFields(rows []WriteRow) []string {
	set := make(map[string]struct{})
	for _, row := range rows {
		for name := range row {
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// writtenFieldTypes 返回各行实际写入的去重字段名及其在表中的解析类型，形如
// "匹配度等级=MultiSelect(4)"，按字段名字典序排序。用于定位 99992402（字段值校验失败）：
// 编码器仅对 Number/DateTime/User 做特殊转换，其余类型按裸字符串写，若目标列实为
// 单选/多选/复选框/关联/公式等，字符串值就与列类型不符而被飞书整批拒绝。
// 表中不存在的字段名标记为 (missing)。
func writtenFieldTypes(rows []WriteRow, fieldTypes map[string]int) []string {
	names := distinctRowFields(rows)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if t, ok := fieldTypes[name]; ok {
			out = append(out, fmt.Sprintf("%s=%s(%d)", name, fieldTypeName(t), t))
			continue
		}
		out = append(out, name+"=(missing)")
	}
	return out
}

// fieldTypeName 把飞书字段类型 ID 映射为可读名称，未知类型返回 "Unknown"。
// 取值对应 larkbitablesdk 的 Type* 常量（见 bitable v1 model）。
func fieldTypeName(t int) string {
	switch t {
	case larkbitablesdk.TypeText:
		return "Text"
	case larkbitablesdk.TypeNumber:
		return "Number"
	case larkbitablesdk.TypeSingleSelect:
		return "SingleSelect"
	case larkbitablesdk.TypeMultiSelect:
		return "MultiSelect"
	case larkbitablesdk.TypeDateTime:
		return "DateTime"
	case larkbitablesdk.TypeCheckbox:
		return "Checkbox"
	case larkbitablesdk.TypeUser:
		return "User"
	case larkbitablesdk.TypePhoneNumber:
		return "PhoneNumber"
	case larkbitablesdk.TypeUrl:
		return "Url"
	case larkbitablesdk.TypeAttachment:
		return "Attachment"
	case larkbitablesdk.TypeLink:
		return "Link"
	case larkbitablesdk.TypeFormula:
		return "Formula"
	case larkbitablesdk.TypeDuplexLink:
		return "DuplexLink"
	case larkbitablesdk.TypeLocation:
		return "Location"
	case larkbitablesdk.TypeGroupChat:
		return "GroupChat"
	case larkbitablesdk.TypeCreatedTime:
		return "CreatedTime"
	case larkbitablesdk.TypeModifiedTime:
		return "ModifiedTime"
	case larkbitablesdk.TypeCreatedUser:
		return "CreatedUser"
	case larkbitablesdk.TypeModifiedUser:
		return "ModifiedUser"
	case larkbitablesdk.TypeAutoSerial:
		return "AutoSerial"
	default:
		return "Unknown"
	}
}
