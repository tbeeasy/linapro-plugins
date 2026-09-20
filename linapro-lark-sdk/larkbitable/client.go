package larkbitable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lina-core/pkg/logger"
	"linapro-lark-sdk/utils"
	"sort"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"

	"linapro-lark-sdk/larkhttp"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	// maxBatchSize 为飞书批量记录接口单次上限（最多 1000 行/批）。
	maxBatchSize = 1000
	// listPageSize 为记录/字段分页拉取的页大小（飞书上限 500）。
	listPageSize = 500
	// maxBatchGetSize 为飞书 records/batch_get 接口单次可传的 record_id 上限（100）。
	maxBatchGetSize = 100
	// throttle 为相邻 Bitable 调用之间的停顿；250ms 在 50 次/秒限流下留足安全余量。
	throttle = 250 * time.Millisecond
)

// Client 使用 app 级凭证读写 Bitable 记录，并按注入的 Encoder 编解码字段值。
// 飞书 SDK 依据 appID/appSecret 自动管理 tenant access token。
type Client struct {
	client  *lark.Client
	encoder Encoder
}

// NewClient 依据飞书 app 凭证构造 Client，使用默认 Encoder（UTC 日期 + Plain 数字 +
// 通用单元格兜底）。调用方如需差异化语义，用 WithEncoder 覆盖。
func NewClient(appID, appSecret string) *Client {
	return &Client{
		client:  lark.NewClient(appID, appSecret, lark.WithHttpClient(larkhttp.NewClient())),
		encoder: DefaultEncoder(),
	}
}

// WithEncoder 返回一个替换了 Encoder 的浅拷贝副本，供调用方注入差异化的日期/数字/
// 单元格兜底语义与人员 id 类型。Encoder 中为 nil 的函数字段会回退到默认实现。
// 人员字段的 open_id 不经 Encoder 注入，由调用方经 CreateOp/UpdateOp.Persons 旁路传入。
func (c *Client) WithEncoder(enc Encoder) *Client {
	cp := *c
	cp.encoder = enc.withDefaults()
	return &cp
}

// UploadMedia 将文件字节作为 Bitable 附件上传到飞书云空间并返回 file_token。文件挂在目标
// Bitable 下（parent_type=bitable_file, parent_node=appToken），以便从记录附件单元格下载。
// size 必须是 r 的精确字节长度。
func (c *Client) UploadMedia(ctx context.Context, appToken, fileName string, size int, r io.Reader) (string, error) {
	req := larkdrive.NewUploadAllMediaReqBuilder().
		Body(larkdrive.NewUploadAllMediaReqBodyBuilder().
			FileName(fileName).
			ParentType(larkdrive.ParentTypeUploadAllMediaBitableFile).
			ParentNode(appToken).
			Size(size).
			File(r).
			Build()).
		Build()
	resp, err := c.client.Drive.V1.Media.UploadAll(ctx, req)
	if err != nil {
		return "", gerror.Wrap(err, "lark: 上传附件请求失败")
	}
	if !resp.Success() {
		return "", gerror.Newf("lark: 上传附件失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil {
		return "", gerror.New("lark: 上传附件未返回数据")
	}
	return larkcore.StringValue(resp.Data.FileToken), nil
}

// ListFields 返回目标表「字段名 → 飞书字段类型 ID」的映射。类型用于让数字与日期字段获得
// 正确的值格式而非字符串。跨多页时翻遍所有页。
func (c *Client) ListFields(ctx context.Context, t Table) (map[string]int, error) {
	fields := make(map[string]int)
	pageToken := ""
	for {
		builder := larkbitablesdk.NewListAppTableFieldReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).PageSize(listPageSize)
		if pageToken != "" {
			builder = builder.PageToken(pageToken)
		}
		resp, err := c.client.Bitable.V1.AppTableField.List(ctx, builder.Build())
		if err != nil {
			return nil, gerror.Wrap(err, "lark: 查询字段请求失败")
		}
		if !resp.Success() {
			return nil, gerror.Newf("lark: 查询字段失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		for _, it := range resp.Data.Items {
			fields[larkcore.StringValue(it.FieldName)] = larkcore.IntValue(it.Type)
		}
		time.Sleep(throttle)
		if resp.Data.HasMore == nil || !*resp.Data.HasMore {
			break
		}
		pageToken = larkcore.StringValue(resp.Data.PageToken)
	}
	return fields, nil
}

// ListRecords 返回全部记录，以 keyOf(row) 的返回值为键。keyOf 返回空串的记录跳过。
// keyOf 通常由调用方通过 KeySpec.KeyOf 构造，支持单列与复合键。
// fieldTypes 用于按列类型解码单元格（人员列按 name 回读、富文本列拼接），
// 必须传 ListFields 同一张表的结果，否则人员列会被按键名嗅探误判。
func (c *Client) ListRecords(ctx context.Context, t Table, keyOf func(Row) string, fieldTypes map[string]int) (map[string]ExistingRecord, error) {
	out := make(map[string]ExistingRecord)
	pageToken := ""
	for {
		builder := larkbitablesdk.NewListAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).PageSize(listPageSize)
		if pageToken != "" {
			builder = builder.PageToken(pageToken)
		}
		resp, err := c.client.Bitable.V1.AppTableRecord.List(ctx, builder.Build())
		if err != nil {
			return nil, gerror.Wrap(err, "lark: 查询记录请求失败")
		}
		if !resp.Success() {
			return nil, gerror.Newf("lark: 查询记录失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}

		for _, rec := range resp.Data.Items {
			row := make(Row, len(rec.Fields))
			persons := map[string][]*larkbitablesdk.Person{}
			for k, v := range rec.Fields {
				ft := fieldTypes[k]
				row[k] = c.cellToString(v, ft)
				if ft == larkbitablesdk.TypeUser {
					if ps := personCellToPersons(v); len(ps) > 0 {
						persons[k] = ps
					}
				}
			}
			key := keyOf(row)
			if key == "" {
				continue
			}
			out[key] = ExistingRecord{
				RecordID: larkcore.StringValue(rec.RecordId),
				Fields:   row,
				Persons:  persons,
			}
		}
		time.Sleep(throttle)
		if resp.Data.HasMore == nil || !*resp.Data.HasMore {
			break
		}
		pageToken = larkcore.StringValue(resp.Data.PageToken)
	}
	return out, nil
}

// ListRecordsByID 返回表中全部记录，以飞书 record_id 为键。
// 返回 ExistingRecord（与 ListRecords 回读语义一致）：文本投影 Fields + 人员列结构化旁路
// Persons —— 对 TypeUser 列另按官方 Person 结构解析出用户身份集合（含 open_id），供调用方对
// 人员列做 open_id 集合比对而非姓名文本比对；无人员列时 Persons 为空。
// fieldTypes 用于按列类型解码单元格，必须传 ListFields 同一张表的结果。
func (c *Client) ListRecordsByID(ctx context.Context, t Table, fieldTypes map[string]int) (map[string]ExistingRecord, error) {
	out := make(map[string]ExistingRecord)
	pageToken := ""
	for {
		builder := larkbitablesdk.NewListAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).PageSize(listPageSize)
		if pageToken != "" {
			builder = builder.PageToken(pageToken)
		}
		resp, err := c.client.Bitable.V1.AppTableRecord.List(ctx, builder.Build())
		if err != nil {
			return nil, gerror.Wrap(err, "lark: 按 ID 查询记录请求失败")
		}
		if !resp.Success() {
			return nil, gerror.Newf("lark: 按 ID 查询记录失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		for _, rec := range resp.Data.Items {
			id := larkcore.StringValue(rec.RecordId)
			if id == "" {
				continue
			}
			row := make(Row, len(rec.Fields))
			persons := map[string][]*larkbitablesdk.Person{}
			for k, v := range rec.Fields {
				ft := fieldTypes[k]
				row[k] = c.cellToString(v, ft)
				if ft == larkbitablesdk.TypeUser {
					if ps := personCellToPersons(v); len(ps) > 0 {
						persons[k] = ps
					}
				}
			}
			out[id] = ExistingRecord{
				RecordID: id,
				Fields:   row,
				Persons:  persons,
			}
		}
		time.Sleep(throttle)
		if resp.Data.HasMore == nil || !*resp.Data.HasMore {
			break
		}
		pageToken = larkcore.StringValue(resp.Data.PageToken)
	}
	return out, nil
}

// BatchGetByIDs 按给定的 record_id 列表精准批量读取记录，避免为拉取少量目标行而全表扫描。
// 内部按 maxBatchGetSize（100）切批调用飞书 records/batch_get，批次间节流。
// 返回命中行映射（以飞书 record_id 为键）与飞书中已不存在的 record_id 列表（absent）。
// 传入空列表时直接返回空结果、不发起请求。
// fieldTypes 用于按列类型解码单元格，必须传 ListFields 同一张表的结果。
func (c *Client) BatchGetByIDs(ctx context.Context, t Table, recordIDs []string, fieldTypes map[string]int) (map[string]Row, []string, error) {
	out := make(map[string]Row, len(recordIDs))
	var absent []string
	if len(recordIDs) == 0 {
		return out, absent, nil
	}

	for start := 0; start < len(recordIDs); start += maxBatchGetSize {
		end := min(start+maxBatchGetSize, len(recordIDs))
		batch := recordIDs[start:end]

		body := larkbitablesdk.NewBatchGetAppTableRecordReqBodyBuilder().
			RecordIds(batch).Build()
		req := larkbitablesdk.NewBatchGetAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).Body(body).Build()

		resp, err := c.client.Bitable.V1.AppTableRecord.BatchGet(ctx, req)
		if err != nil {
			return nil, nil, gerror.Wrap(err, "lark: 批量获取记录请求失败")
		}
		if !resp.Success() {
			return nil, nil, gerror.Newf("lark: 批量获取记录失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}

		for _, rec := range resp.Data.Records {
			id := larkcore.StringValue(rec.RecordId)
			if id == "" {
				continue
			}
			row := make(Row, len(rec.Fields))
			for k, v := range rec.Fields {
				row[k] = c.cellToString(v, fieldTypes[k])
			}
			out[id] = row
		}
		absent = append(absent, resp.Data.AbsentRecordIds...)

		time.Sleep(throttle)
	}
	return out, absent, nil
}

// userIDType 返回下发飞书的 user_id_type 查询参数值，空则兜底 open_id。
// userIDType 返回配置的 user_id_type 及是否应下发该查询参数。
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
