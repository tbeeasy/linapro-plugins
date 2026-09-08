package moka

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

const stagesListPath = "/api-platform/v2/stage/getStagesList"

// Stage 是 Moka 招聘流水线的一个阶段。常见 Type 取值：
// 100=初筛, 201=面试轮次, 102=待入职 等。
type Stage struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type stagesEnvelope struct {
	Code int     `json:"code"`
	Msg  string  `json:"msg"`
	Data []Stage `json:"data"`
}

// GetStagesList 拉取组织的全部招聘流水线阶段。
// 调用方用它把可读的阶段名称（如 "初筛"）解析为 EhrApplications 和
// MoveApplicationStage 所需的数字 ID。
func (c *Client) GetStagesList(ctx context.Context) ([]Stage, error) {
	raw, err := c.GetJSON(ctx, stagesListPath)
	if err != nil {
		return nil, err
	}

	var env stagesEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: decode getStagesList response: %s", truncate(string(raw), 512))
	}
	if env.Code != 200 {
		return nil, gerror.Newf("moka-recruit: getStagesList returned code=%d msg=%q", env.Code, env.Msg)
	}
	return env.Data, nil
}
