// apikey_v1_get.go 实现签名凭据详情与查看 secret 接口。

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"
)

// Get 返回单条签名凭据（不含 secret）。
func (c *ControllerV1) Get(ctx context.Context, req *v1.GetReq) (res *v1.GetRes, err error) {
	item, err := c.apiKeySvc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.GetRes{Item: toItem(item)}, nil
}

// GetSecret 返回签名凭据的 secret 明文（独立权限点保护）。
func (c *ControllerV1) GetSecret(ctx context.Context, req *v1.GetSecretReq) (res *v1.GetSecretRes, err error) {
	secret, err := c.apiKeySvc.GetSecret(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.GetSecretRes{Secret: secret}, nil
}
