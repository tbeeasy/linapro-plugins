// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package push

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/push/v1"
)

type IPushV1 interface {
	SendFile(ctx context.Context, req *v1.SendFileReq) (res *v1.SendFileRes, err error)
	Status(ctx context.Context, req *v1.StatusReq) (res *v1.StatusRes, err error)
	SendText(ctx context.Context, req *v1.SendTextReq) (res *v1.SendTextRes, err error)
}
