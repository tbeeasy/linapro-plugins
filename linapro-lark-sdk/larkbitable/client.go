package larkbitable

import (
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"linapro-lark-sdk/larkhttp"
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
