// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"
)

type IApikeyV1 interface {
	Create(ctx context.Context, req *v1.CreateReq) (res *v1.CreateRes, err error)
	Delete(ctx context.Context, req *v1.DeleteReq) (res *v1.DeleteRes, err error)
	Get(ctx context.Context, req *v1.GetReq) (res *v1.GetRes, err error)
	GetSecret(ctx context.Context, req *v1.GetSecretReq) (res *v1.GetSecretRes, err error)
	List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error)
	ListGroups(ctx context.Context, req *v1.ListGroupsReq) (res *v1.ListGroupsRes, err error)
	Update(ctx context.Context, req *v1.UpdateReq) (res *v1.UpdateRes, err error)
	UpdateStatus(ctx context.Context, req *v1.UpdateStatusReq) (res *v1.UpdateStatusRes, err error)
}
