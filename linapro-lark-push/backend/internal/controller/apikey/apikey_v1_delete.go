// apikey_v1_delete.go 实现删除签名凭据接口。

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"
)

// Delete 软删除签名凭据。
func (c *ControllerV1) Delete(ctx context.Context, req *v1.DeleteReq) (res *v1.DeleteRes, err error) {
	if err = c.apiKeySvc.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteRes{}, nil
}
