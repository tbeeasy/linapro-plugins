// Package larkattend 封装飞书假勤（attendance v1）OpenAPI 的二次调用：
// 考勤组、班次、排班、打卡结果与打卡流水，作为共享库供考勤类插件复用。
//
// 它复用同库 larkhttp 的 429 限流退避重试，不重复实现 token 管理——
// tenant_access_token 的获取与刷新由官方 SDK 经注入的 larkhttp.Client 自动处理
// （SDK 的 token 请求与业务请求走同一个 HttpClient，故 token 刷新被限流时同样退避重试）。
//
// 除 HTTP 429 之外，飞书还会返回 HTTP 200 + 业务码非 0 的限流与瞬时故障
// （99991400 限流、99991401 服务繁忙、999999 内部错误）。这类响应不会被 larkhttp 处理，
// 由本包的 callWithBackoff 统一退避重试，重试用尽再转成 *FeishuError。
//
// 边界约束：
//   - 只做读接口（列表、详情、批量查询），MUST NOT 实现写接口（建组、排班、导入流水）
//     与事件订阅；
//   - 库内依赖方向为 larkattend → larkhttp；
//   - 允许引用 lina-core/pkg 的通用能力包（logger），MUST NOT 引用 lina-core 的业务 service；
//   - MUST NOT 让 lina-core 反向依赖 larksuite SDK。
package larkattend

import (
	"context"
	"time"

	larkclient "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larksdk "github.com/larksuite/oapi-sdk-go/v3/service/attendance/v1"

	"linapro-lark-sdk/larkhttp"
)

const (
	// batchSize 是飞书按人批量查询接口（user_tasks / user_daily_shifts / user_flows）
	// 的单次人数上限。三种接口都是 50 人，统一按 50 切片，不做上限探测。
	batchSize = 50

	// maxRetries 是业务码可重试错误的额外重试次数（不含首次请求）。
	maxRetries = 3
	// backoffBase 是业务码退避的基准时长，按 2 的幂次增长。
	backoffBase = 2 * time.Second
	// backoffMax 是单次业务码退避的等待上限。
	backoffMax = 30 * time.Second
)

// groupAPI 是「查询所有考勤组 / 按 ID 查询考勤组 / 查询考勤组成员」的最小接缝，官方 SDK 的 Group
// 结构化满足它。抽成接口只为让单测能注入假实现，生产路径始终走官方 SDK。
type groupAPI interface {
	List(ctx context.Context, req *larksdk.ListGroupReq, options ...larkcore.RequestOptionFunc) (*larksdk.ListGroupResp, error)
	Get(ctx context.Context, req *larksdk.GetGroupReq, options ...larkcore.RequestOptionFunc) (*larksdk.GetGroupResp, error)
	ListUser(ctx context.Context, req *larksdk.ListUserGroupReq, options ...larkcore.RequestOptionFunc) (*larksdk.ListUserGroupResp, error)
}

// shiftAPI 是「查询所有班次」的最小接缝。
type shiftAPI interface {
	List(ctx context.Context, req *larksdk.ListShiftReq, options ...larkcore.RequestOptionFunc) (*larksdk.ListShiftResp, error)
}

// userTaskAPI 是「查询打卡结果」的最小接缝。
type userTaskAPI interface {
	Query(ctx context.Context, req *larksdk.QueryUserTaskReq, options ...larkcore.RequestOptionFunc) (*larksdk.QueryUserTaskResp, error)
}

// userDailyShiftAPI 是「查询排班」的最小接缝。
type userDailyShiftAPI interface {
	Query(ctx context.Context, req *larksdk.QueryUserDailyShiftReq, options ...larkcore.RequestOptionFunc) (*larksdk.QueryUserDailyShiftResp, error)
}

// userFlowAPI 是「查询打卡流水」的最小接缝。
type userFlowAPI interface {
	Query(ctx context.Context, req *larksdk.QueryUserFlowReq, options ...larkcore.RequestOptionFunc) (*larksdk.QueryUserFlowResp, error)
}

// Client 使用一个飞书应用的凭据调用假勤接口，复用 larkhttp 的 429 限流退避。
// 官方 SDK 依据 appID/appSecret 自动管理 tenant access token；每个 Client 各持一份
// 进程内 token 缓存，飞书允许同一应用多个 token 同时有效，因此无需跨实例共享。
type Client struct {
	// groups / shifts / userTasks / userDailyShifts / userFlows 是各接口接缝，
	// 由构造函数绑定到官方 SDK；单测可替换。
	groups          groupAPI
	shifts          shiftAPI
	userTasks       userTaskAPI
	userDailyShifts userDailyShiftAPI
	userFlows       userFlowAPI
	// sleep 抽象退避等待，单测可替换为立即返回；nil 时使用默认实现。
	sleep func(context.Context, time.Duration) error
}

// NewClient 依据飞书 app 凭据构造 Client，注入 larkhttp 的 429 退避 HttpClient。
func NewClient(appID, appSecret string) *Client {
	return newClient(appID, appSecret, "")
}

// newClient 构造 Client；baseURL 非空时把资源请求与换票请求都指向它，
// 供包内测试用 httptest 打桩，生产路径始终传空串使用飞书生产地址。
//
// 这里直接构造 larkcore.Config 而不是先 larkclient.NewClient 再取内部 config：SDK 的
// Client.config 字段不导出，而资源请求走 Config.BaseUrl、换 token 走 Config.OAuthBaseUrl，
// 打桩必须能同时改这两项。Config 的关键字段与官方 NewClient 的默认值保持一致，并同样
// 补齐 Logger / Cache / Serialization。
func newClient(appID, appSecret, baseURL string) *Client {
	config := &larkcore.Config{
		BaseUrl:          larkclient.FeishuBaseUrl,
		OAuthBaseUrl:     larkclient.FeishuBaseUrl,
		AppId:            appID,
		AppSecret:        appSecret,
		EnableTokenCache: true,
		AppType:          larkcore.AppTypeSelfBuilt,
		HttpClient:       larkhttp.NewClient(),
	}
	if baseURL != "" {
		config.BaseUrl = baseURL
		config.OAuthBaseUrl = baseURL
	}
	larkcore.NewLogger(config)
	larkcore.NewCache(config)
	larkcore.NewSerialization(config)

	v1 := larksdk.New(config)
	return &Client{
		groups:          v1.Group,
		shifts:          v1.Shift,
		userTasks:       v1.UserTask,
		userDailyShifts: v1.UserDailyShift,
		userFlows:       v1.UserFlow,
		sleep:           sleepWithContext,
	}
}

// callWithBackoff 执行一次飞书调用，并对可重试的业务错误码退避重试。
//
// call 返回本次调用的（HTTP 响应, 飞书业务错误码, 传输层错误）三元组。传输层错误
// （网络失败、HTTP 非 2xx）与业务码非 0 都会进入重试判定：可重试的错误码（限流、
// 服务繁忙、内部错误）按 2 的幂次退避后重发，其余直接返回 *FeishuError。
// 重试用尽或 ctx 取消时返回最后一次的 *FeishuError，调用方可用 errors.As 取 code / req_id。
//
// 本函数不持有响应体：成功时调用方已经拿到了需要的响应对象，逐层透传泛型响应只会
// 让每个接口多一份类型参数，收益为零。
func callWithBackoff(
	ctx context.Context,
	sleep func(context.Context, time.Duration) error,
	action string,
	call func() (*larkcore.ApiResp, larkcore.CodeError, error),
) error {
	var last *FeishuError
	for attempt := 0; ; attempt++ {
		apiResp, codeErr, err := call()
		switch {
		case err != nil:
			last = newFeishuError(action, 0, err.Error(), requestID(apiResp))
		case codeErr.Code != 0:
			last = newFeishuError(action, codeErr.Code, codeErr.Msg, requestID(apiResp))
		default:
			return nil
		}
		retryable := codeErr.Code == 0 || retryableCode(codeErr.Code)
		if attempt >= maxRetries || !retryable || ctx.Err() != nil {
			return last
		}
		if sleepErr := sleep(ctx, waitDuration(attempt)); sleepErr != nil {
			return last
		}
	}
}

// sleepOrDefault 返回注入的等待实现，未注入时使用默认实现。
func (c *Client) sleepOrDefault() func(context.Context, time.Duration) error {
	if c.sleep != nil {
		return c.sleep
	}
	return sleepWithContext
}

// retryableCode 判断业务错误码是否属于可重试的限流或瞬时故障。
func retryableCode(code int) bool {
	switch code {
	case codeRateLimited, codeServiceTooBusy, codeInternalError:
		return true
	default:
		return false
	}
}

// waitDuration 返回第 attempt 次重试的等待时长：base*2^attempt，上限 backoffMax。
func waitDuration(attempt int) time.Duration {
	wait := backoffBase << attempt
	if wait > backoffMax {
		return backoffMax
	}
	return wait
}

// sleepWithContext 等待 d，或在 ctx 取消时提前返回。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// requestID 从 ApiResp 中取飞书请求 ID，供调用方向飞书侧追查。
func requestID(apiResp *larkcore.ApiResp) string {
	if apiResp == nil {
		return ""
	}
	return apiResp.RequestId()
}

// 指针字段的零值安全读取。
func stringValue[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func intValue[T ~int](p *T) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

func boolValue(p *bool) bool {
	return p != nil && *p
}

func floatValue(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
