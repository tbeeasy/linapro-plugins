// middleware_record_test.go 用一次真实的 ghttp 分发走通 Record 全链路，锁定三件事：
// 处理器已用 Parse() 读过的 JSON 请求体仍能完整取到；request_at 是请求开始时间而不是写库时间；
// 写库失败不改变已经发出的响应。

package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/guid"

	"lina-core/pkg/plugin/pluginhost"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
)

// recordingApiLogSvc 是写入服务替身：把收到的写入内容投递到通道，并按配置返回错误。
type recordingApiLogSvc struct {
	inputs    chan apilogsvc.CreateInput
	createErr error
}

// Create 记下写入内容并返回预设错误。
func (s *recordingApiLogSvc) Create(_ context.Context, in apilogsvc.CreateInput) error {
	s.inputs <- in
	return s.createErr
}

// List 本测试不涉及。
func (s *recordingApiLogSvc) List(_ context.Context, _ apilogsvc.ListInput) (*apilogsvc.ListOutput, error) {
	return nil, errors.New("测试替身不支持 List")
}

// Get 本测试不涉及。
func (s *recordingApiLogSvc) Get(_ context.Context, _ apilogsvc.GetInput) (*apilogsvc.ApiLogEntity, error) {
	return nil, errors.New("测试替身不支持 Get")
}

// TestRecordCapturesParsedBodyAndKeepsResponseOnPersistFailure 走通一次「被记录」的请求。
func TestRecordCapturesParsedBodyAndKeepsResponseOnPersistFailure(t *testing.T) {
	const (
		ownerPlugin  = "linapro-eval-360"
		probePath    = "/x/linapro-eval-360/api/v1/submit"
		payload      = `{"taskId":7,"answers":[{"q":1,"v":"A"}]}`
		responseText = `{"code":0,"message":"ok"}`
		handlerDelay = 20 * time.Millisecond
	)
	apiLogSvc := &recordingApiLogSvc{
		inputs:    make(chan apilogsvc.CreateInput, 1),
		createErr: errors.New("模拟写库失败"),
	}
	svc := &serviceImpl{
		bizCtxSvc:  fakeBizCtx{},
		hostConfig: &fakeHostConfig{values: map[string]any{}},
		apiLogSvc:  apiLogSvc,
	}

	server := ghttp.GetServer("apilog-record-" + guid.S())
	server.SetPort(0)
	server.SetDumpRouterMap(false)
	var handlerEnteredAt time.Time
	server.Group("/", func(group *ghttp.RouterGroup) {
		// 记录中间件在最外层；归属插件标记模拟宿主为源码插件路由挂的那层。
		group.Middleware(svc.Record, func(r *ghttp.Request) {
			pluginhost.SetSourcePluginIDForRequest(r, ownerPlugin)
			r.Middleware.Next()
		})
		group.POST(probePath, func(r *ghttp.Request) {
			handlerEnteredAt = time.Now()
			var body struct {
				TaskID int `json:"taskId"`
			}
			if err := r.Parse(&body); err != nil {
				r.Response.WriteStatus(http.StatusBadRequest, err.Error())
				return
			}
			// 处理器耗时：request_at 必须早于它，而写库时刻必然晚于它。
			time.Sleep(handlerDelay)
			r.Response.Header().Set("Content-Type", "application/json")
			r.Response.Write(responseText)
		})
	})
	if err := server.Start(); err != nil {
		t.Fatalf("启动测试服务失败: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Shutdown(); err != nil {
			t.Fatalf("关闭测试服务失败: %v", err)
		}
	})

	response, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d%s", server.GetListenedPort(), probePath),
		"application/json",
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("请求测试服务失败: %v", err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("关闭响应体失败: %v", closeErr)
	}
	if readErr != nil {
		t.Fatalf("读取响应体失败: %v", readErr)
	}

	// 写库失败不改变响应：状态码与正文都是处理器给出的原样。
	if response.StatusCode != http.StatusOK || string(responseBody) != responseText {
		t.Fatalf("写库失败不应改变响应，实际 status=%d body=%q", response.StatusCode, responseBody)
	}

	var input apilogsvc.CreateInput
	select {
	case input = <-apiLogSvc.inputs:
	case <-time.After(5 * time.Second):
		t.Fatal("命中记录条件的请求应触发一次写入")
	}
	if input.RouteOwner != ownerPlugin {
		t.Fatalf("归属插件应为 %s，实际为 %q", ownerPlugin, input.RouteOwner)
	}
	// 处理器已用 Parse() 读过 Body，记录仍应拿到完整内容。
	if input.ReqBody != payload {
		t.Fatalf("请求体应完整保留，期望 %q，实际 %q", payload, input.ReqBody)
	}
	if input.RespBody != responseText {
		t.Fatalf("响应体应原样记录，期望 %q，实际 %q", responseText, input.RespBody)
	}
	// request_at 是请求开始时间：不晚于进入处理器的时刻，且 cost_ms 覆盖了处理器耗时。
	if input.RequestAt.IsZero() || input.RequestAt.After(handlerEnteredAt) {
		t.Fatalf("request_at 应不晚于进入处理器的时刻 %v，实际 %v", handlerEnteredAt, input.RequestAt)
	}
	if time.Duration(input.CostMs)*time.Millisecond < handlerDelay {
		t.Fatalf("cost_ms 应覆盖处理器耗时 %v，实际 %dms", handlerDelay, input.CostMs)
	}
}
