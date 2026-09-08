package hcm

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

const reportDataPath = "/api-platform/hcm/oapi/v1/report/getReportData"

// APICodeKeyReportData 是 getReportData 接口在 APICodes 映射中的键。
// 使用方必须将 Moka 为该接口下发的 apiCode 填入
// HCMCredential.APICodes[APICodeKeyReportData]。
const APICodeKeyReportData = "reportData"

// reportSuccessCode 是 getReportData 文档声明的成功码
// （实测值为 200；出于文档文本冲突的防御，1000000 也被接受）。
const reportSuccessCode = 200
const legacyReportSuccessCode = 1000000

// ReportHeader 是报表的一列表头。Children 用于建模多级表头；
// 集成不使用它，但保留该字段以确保解码时不会静默丢弃数据。
type ReportHeader struct {
	DataIndex string         `json:"dataIndex"`
	Title     string         `json:"title"`
	Type      string         `json:"type"`
	Children  []ReportHeader `json:"children,omitempty"`
}

// ReportData 是 getReportData 响应的数据载荷。
type ReportData struct {
	Headers []ReportHeader   `json:"headers"`
	Rows    []map[string]any `json:"rows"`
	Size    int              `json:"size"`
}

type reportEnvelope struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data *ReportData `json:"data"`
}

// GetReportData 按 id 查询单个 Moka 报表。当返回非成功码时，
// 返回携带 API msg 的错误，便于调用方记录日志并跳过。
func (c *Client) GetReportData(ctx context.Context, reportID int64) (*ReportData, error) {
	body, err := json.Marshal(map[string]int64{"reportId": reportID})
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: marshal getReportData body")
	}

	// 打印请求参数
	glog.Debugf(ctx, "moka-hcm: GetReportData 请求参数 %s", string(body))

	raw, err := c.PostJSON(ctx, reportDataPath, c.cred.APICodes[APICodeKeyReportData], body, nil)
	if err != nil {
		return nil, err
	}

	// 打印原始返回值
	glog.Debugf(ctx, "moka-hcm: GetReportData 原始请求返回值 %s", string(raw))

	var env reportEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "hcm: decode getReportData response: %s", truncate(string(raw), 512))
	}
	if env.Code != reportSuccessCode && env.Code != legacyReportSuccessCode {
		return nil, gerror.Newf("hcm: getReportData reportId=%d returned code=%d msg=%q",
			reportID, env.Code, env.Msg)
	}
	if env.Data == nil {
		return &ReportData{}, nil
	}

	glog.Debugf(ctx, "moka-hcm: GetReportData 格式化之后请求返回值 %+v", env.Data)

	return env.Data, nil
}
