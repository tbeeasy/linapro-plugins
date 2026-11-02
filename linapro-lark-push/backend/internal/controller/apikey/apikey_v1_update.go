// apikey_v1_update.go 实现编辑签名凭据与启停接口。

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"

	apikeysvc "lina-plugin-linapro-lark-push/backend/internal/service/apikey"
)

// Update 编辑签名凭据；secret 为空时保持原值。
func (c *ControllerV1) Update(ctx context.Context, req *v1.UpdateReq) (res *v1.UpdateRes, err error) {
	err = c.apiKeySvc.Update(ctx, req.Id, apikeysvc.SaveInput{
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
	return &v1.UpdateRes{}, nil
}

// UpdateStatus 启用或停用签名凭据。
func (c *ControllerV1) UpdateStatus(ctx context.Context, req *v1.UpdateStatusReq) (res *v1.UpdateStatusRes, err error) {
	if err = c.apiKeySvc.SetStatus(ctx, req.Id, apikeysvc.Status(req.Status)); err != nil {
		return nil, err
	}
	return &v1.UpdateStatusRes{}, nil
}
