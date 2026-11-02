// apikey_v1_create.go 实现新增签名凭据接口。

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"

	apikeysvc "lina-plugin-linapro-lark-push/backend/internal/service/apikey"
)

// Create 新增签名凭据，返回最终保存的 secret。
func (c *ControllerV1) Create(ctx context.Context, req *v1.CreateReq) (res *v1.CreateRes, err error) {
	out, err := c.apiKeySvc.Create(ctx, apikeysvc.SaveInput{
		Name:          req.Name,
		AppKey:        req.AppKey,
		Secret:        req.Secret,
		Group:         req.Group,
		ChatID:        req.ChatId,
		ChatPushAppID: req.ChatPushAppId,
		Status:        apikeysvc.Status(req.Status),
		Remark:        req.Remark,
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{Id: out.Id, Secret: out.Secret}, nil
}
