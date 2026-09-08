package moka

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

const reportDataPath = "/api-platform/v1/getReportData"

// reportSuccessCode 是 getReportData 文档标注的成功码（实际观察值为 200；
// 鉴于文档表述冲突，额外容忍 1000000）。
const reportSuccessCode = 200
const legacyReportSuccessCode = 1000000

// ReportHeader 是报表的一个列头。Children 用于建模多级表头；
// 集成层不使用它们，但保留该字段以免解析时静默丢弃数据。
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

// GetReportData 按 id 查询一个 Moka 招聘报表。返回非成功码时，
// 返回携带 API msg 的错误，调用方可记录日志并跳过。
func (c *Client) GetReportData(ctx context.Context, reportID int64) (*ReportData, error) {
	body, err := json.Marshal(map[string]int64{"reportId": reportID})
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: marshal getReportData body")
	}

	// 打印请求参数
	glog.Debugf(ctx, "moka-recruit: GetReportData 请求参数 %s", string(body))

	raw, err := c.PostJSON(ctx, reportDataPath, body)
	if err != nil {
		return nil, err
	}

	// 打印原始返回值
	// glog.Debugf(ctx, "moka-recruit: GetReportData 原始请求返回值 %s", string(raw))

	var env reportEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: decode getReportData response: %s", truncate(string(raw), 512))
	}
	if env.Code != reportSuccessCode && env.Code != legacyReportSuccessCode {
		return nil, gerror.Newf("moka-recruit: getReportData reportId=%d returned code=%d msg=%q",
			reportID, env.Code, env.Msg)
	}
	if env.Data == nil {
		return &ReportData{}, nil
	}
	// 打印返回值
	glog.Debugf(ctx, "moka-recruit: GetReportData 格式化之后请求返回值 %+v", env.Data)

	return env.Data, nil
}
