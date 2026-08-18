package hcm

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

const reportDataPath = "/api-platform/hcm/oapi/v1/report/getReportData"

// reportSuccessCode is the documented success code for getReportData (observed
// value is 200; 1000000 is accepted defensively per conflicting doc text).
const reportSuccessCode = 200
const legacyReportSuccessCode = 1000000

// ReportHeader is one report column header. Children models multi-level
// headers; the integration does not use them but preserves the field so the
// decode never silently drops data.
type ReportHeader struct {
	DataIndex string         `json:"dataIndex"`
	Title     string         `json:"title"`
	Type      string         `json:"type"`
	Children  []ReportHeader `json:"children,omitempty"`
}

// ReportData is the data payload of a getReportData response.
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

// GetReportData queries one Moka report by id. On a non-success code it
// returns an error carrying the API msg so the caller can log and skip.
func (c *Client) GetReportData(ctx context.Context, reportID int64) (*ReportData, error) {
	body, err := json.Marshal(map[string]int64{"reportId": reportID})
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: marshal getReportData body")
	}

	raw, err := c.PostJSON(ctx, reportDataPath, body)
	if err != nil {
		return nil, err
	}

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
	return env.Data, nil
}
