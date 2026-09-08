package moka

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

const resumeContentPath = "/api-platform/application/resumeContent/get"

// ResumeContent 保存 Moka 返回的解析后简历文本。
// ResumeKey 是原始文件的对象存储键；ResumeContent 是供 AI 流水线读取的
// 提取出的纯文本。
type ResumeContent struct {
	ResumeKey     string `json:"resumeKey"`
	ResumeContent string `json:"resumeContent"`
}

type resumeEnvelope struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data *ResumeContent `json:"data"`
}

// GetResumeContent 拉取单个候选人的纯文本简历。
// 它不下载原始文件；Moka 返回的是预先解析好的文本。
// 非 200 业务码会作为携带 API msg 的错误返回。
func (c *Client) GetResumeContent(ctx context.Context, applicationID int64) (*ResumeContent, error) {
	body, err := json.Marshal(map[string]int64{"applicationId": applicationID})
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: marshal resumeContent body")
	}

	raw, err := c.PostJSON(ctx, resumeContentPath, body)
	if err != nil {
		return nil, err
	}

	var env resumeEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: decode resumeContent response: %s", truncate(string(raw), 512))
	}
	if env.Code != 200 {
		return nil, gerror.Newf("moka-recruit: resumeContent applicationId=%d returned code=%d msg=%q",
			applicationID, env.Code, env.Msg)
	}
	if env.Data == nil {
		return &ResumeContent{}, nil
	}
	return env.Data, nil
}
