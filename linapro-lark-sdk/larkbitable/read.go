// read.go 提供 Bitable 的读取：字段列举、记录全量分页拉取（按业务键或 record_id 建索引）
// 与按 record_id 精准批量读取。分页与批次之间均按 throttle 节流。

package larkbitable

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"linapro-lark-sdk/utils"
)

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
// 请求带 automatic_fields=true，额外回带记录创建时间（CreatedTime），供消费方按插入时间过滤。
// fieldTypes 用于按列类型解码单元格，必须传 ListFields 同一张表的结果。
func (c *Client) ListRecordsByID(ctx context.Context, t Table, fieldTypes map[string]int) (map[string]ExistingRecord, error) {
	out := make(map[string]ExistingRecord)
	pageToken := ""
	for {
		builder := larkbitablesdk.NewListAppTableRecordReqBuilder().
			AppToken(t.AppToken).TableId(t.TableID).PageSize(listPageSize).
			AutomaticFields(true)
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
				RecordID:    id,
				Fields:      row,
				Persons:     persons,
				CreatedTime: createdTimeMillis(larkcore.Int64Value(rec.CreatedTime)),
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

// createdTimeMillis 把飞书回带的记录创建时间归一为毫秒时间戳。飞书文档虽标称毫秒，
// 但不同接口/历史数据存在秒级值与毫秒级值并存的情况，故此处按量级判定：
// 小于 1e12（约公元 2001 年）的视为秒，乘以 1000 归一到毫秒；其余原样返回。0 保持 0。
func createdTimeMillis(v int64) int64 {
	if v > 0 && v < 1e12 {
		return v * 1000
	}
	return v
}
