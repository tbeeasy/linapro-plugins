// push_v1_status.go 实现 GET /open/v1/status：部署验收与告警用的自检接口。

package push

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"

	v1 "lina-plugin-linapro-lark-push/backend/api/push/v1"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// Status 返回推送能力自检报告，不发送任何消息；推送能力未就绪时返回 HTTP 503 与
// LINAPRO_LARK_PUSH_SENDER_UNBOUND（原因由错误码与本地化 message 表达，data 为空）。
func (c *ControllerV1) Status(ctx context.Context, _ *v1.StatusReq) (res *v1.StatusRes, err error) {
	if c.sender == nil {
		return nil, larkpushcode.WithHTTPStatus(g.RequestFromCtx(ctx), bizerr.NewCode(larkpushcode.CodeSenderUnbound))
	}
	return toStatusResponse(c.sender.Status(ctx)), nil
}

// toStatusResponse 把自检报告投影为接口结果。
func toStatusResponse(report larkpushcap.StatusReport) *v1.StatusRes {
	res := &v1.StatusRes{
		PluginReady:  report.PluginReady,
		ConfigValid:  report.ConfigValid,
		ConfigIssues: report.ConfigIssues,
		PushApps:     make([]*v1.PushAppStatusRes, 0, len(report.PushApps)),
	}
	for _, app := range report.PushApps {
		res.PushApps = append(res.PushApps, &v1.PushAppStatusRes{
			Group:                app.Group,
			AppID:                app.AppID,
			Name:                 app.Name,
			IdentityAppID:        app.IdentityAppID,
			CredentialReady:      app.CredentialReady,
			AddressableEmployees: app.AddressableEmployees,
			BlankUserIDRows:      app.BlankUserIDEmployees,
			Collisions:           app.Collisions,
			ProbeError:           app.ProbeError,
		})
	}
	return res
}
