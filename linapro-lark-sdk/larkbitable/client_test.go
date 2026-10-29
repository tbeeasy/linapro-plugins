package larkbitable

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// fakeHTTP 实现 larkcore.HttpClient（仅需 Do 方法），拦截飞书请求：
// 鉴权请求返回固定 token；batch_get 请求按传入的 record_ids 回放命中行，
// 并把 absentSet 中的 id 放入 absent_record_ids。同时记录每批传入的 record_ids
// 以便断言切批行为。
type fakeHTTP struct {
	// batchCalls 记录每次 batch_get 收到的 record_ids（按调用顺序）。
	batchCalls [][]string
	// rows 为全部可命中的行，按 record_id 索引。
	rows map[string]map[string]any
	// absentSet 中的 record_id 会被归入 absent_record_ids，不出现在 records。
	absentSet map[string]bool
	// writeCalls 记录每次 batch_create/batch_update 收到的 records 数组（按调用顺序），
	// 供断言空字段记录被跳过、以及全空批次不发起请求。
	writeCalls [][]map[string]any
	// writeUserIDTypes 记录每次写入请求下发的 user_id_type 查询参数（按调用顺序）。
	// 人员字段以何种 id 语义写入由它决定，空串表示未下发该参数。
	writeUserIDTypes []string
	// listRecordsRows 为 list（全量列举）接口的预设行，格式为
	// []map[string]any，每项含 "record_id" 和 "fields" 两个键。
	listRecordsRows []map[string]any
}

func (f *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	path := req.URL.Path

	// 鉴权：返回固定 tenant_access_token。
	if strings.HasSuffix(path, "/auth/v3/tenant_access_token/internal") ||
		strings.HasSuffix(path, "/auth/v3/app_access_token/internal") {
		return jsonResp(`{"code":0,"msg":"ok","tenant_access_token":"t","app_access_token":"t","expire":7200}`), nil
	}

	// batch_get：解析 record_ids，回放命中行与 absent。
	if strings.HasSuffix(path, "/records/batch_get") {
		var body struct {
			RecordIds []string `json:"record_ids"`
		}
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)
		f.batchCalls = append(f.batchCalls, body.RecordIds)

		type recordJSON struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		}
		var records []recordJSON
		var absent []string
		for _, id := range body.RecordIds {
			if f.absentSet[id] {
				absent = append(absent, id)
				continue
			}
			if fields, ok := f.rows[id]; ok {
				records = append(records, recordJSON{RecordID: id, Fields: fields})
			}
		}
		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"records":           records,
				"absent_record_ids": absent,
			},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// list（全量拉取）：回放 listRecordsRows，不分页。
	if strings.Contains(path, "/records") && req.Method == http.MethodGet {
		type recJSON struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		}
		records := make([]recJSON, 0, len(f.listRecordsRows))
		for _, r := range f.listRecordsRows {
			id, _ := r["record_id"].(string)
			fields, _ := r["fields"].(map[string]any)
			records = append(records, recJSON{RecordID: id, Fields: fields})
		}
		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"items":    records,
				"has_more": false,
			},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// batch_create/batch_update：记录本次请求的 records（含各记录 fields），
	// 回放一批与请求等长的 record_id，供断言空字段记录被跳过。
	if strings.HasSuffix(path, "/records/batch_create") || strings.HasSuffix(path, "/records/batch_update") {
		var body struct {
			Records []struct {
				Fields map[string]any `json:"fields"`
			} `json:"records"`
		}
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)

		fieldsList := make([]map[string]any, 0, len(body.Records))
		records := make([]map[string]any, 0, len(body.Records))
		for i, r := range body.Records {
			fieldsList = append(fieldsList, r.Fields)
			records = append(records, map[string]any{"record_id": "new" + itoa(i)})
		}
		f.writeCalls = append(f.writeCalls, fieldsList)
		f.writeUserIDTypes = append(f.writeUserIDTypes, req.URL.Query().Get("user_id_type"))

		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{"records": records},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	return jsonResp(`{"code":0,"msg":"ok"}`), nil
}

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
}

// newTestClient 构造一个注入了 fake HttpClient 的 Client（同包可直接装配私有字段）。
func newTestClient(f *fakeHTTP) *Client {
	return &Client{
		client:  lark.NewClient("id", "secret", lark.WithHttpClient(f)),
		encoder: DefaultEncoder(),
	}
}

// fieldTypesEmpty 返回空字段类型映射。测试中这些记录仅含纯字符串/数字单元格，无需类型
// 即可正确回读；空/未知类型统一走 CellFallback。
func fieldTypesEmpty() map[string]int {
	return map[string]int{}
}

// itoa 是 strconv.Itoa 的短别名，便于批量构造测试 record_id。
func itoa(i int) string {
	return strconv.Itoa(i)
}
