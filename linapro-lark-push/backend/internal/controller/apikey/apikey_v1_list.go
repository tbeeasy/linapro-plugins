// apikey_v1_list.go 实现签名凭据分页列表与推送分组下拉接口。

package apikey

import (
	"context"

	"lina-plugin-linapro-lark-push/backend/api/apikey/v1"

	apikeysvc "lina-plugin-linapro-lark-push/backend/internal/service/apikey"
)

// List 分页查询签名凭据（不含 secret）。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	in := apikeysvc.ListInput{
		Keyword:  req.Keyword,
		Group:    req.Group,
		PageNum:  req.PageNum,
		PageSize: req.PageSize,
	}
	if req.Status != nil {
		status := apikeysvc.Status(*req.Status)
		in.Status = &status
	}
	out, err := c.apiKeySvc.List(ctx, in)
	if err != nil {
		return nil, err
	}
	res = &v1.ListRes{List: make([]*v1.Item, 0, len(out.List)), Total: out.Total}
	for _, item := range out.List {
		res.List = append(res.List, toItem(item))
	}
	return res, nil
}

// ListGroups 返回 push_groups 中已配置的分组及各自的推送应用。
func (c *ControllerV1) ListGroups(ctx context.Context, _ *v1.ListGroupsReq) (res *v1.ListGroupsRes, err error) {
	groups, err := c.apiKeySvc.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	res = &v1.ListGroupsRes{List: make([]*v1.GroupItem, 0, len(groups))}
	for _, group := range groups {
		item := &v1.GroupItem{Name: group.Name, PushApps: make([]*v1.PushAppItem, 0, len(group.PushApps))}
		for _, app := range group.PushApps {
			item.PushApps = append(item.PushApps, &v1.PushAppItem{AppId: app.AppID, Name: app.Name})
		}
		res.List = append(res.List, item)
	}
	return res, nil
}

// toItem 把服务层视图投影为接口 DTO。
func toItem(item *apikeysvc.Item) *v1.Item {
	return &v1.Item{
		Id:            item.Id,
		Name:          item.Name,
		AppKey:        item.AppKey,
		Group:         item.Group,
		ChatId:        item.ChatID,
		ChatPushAppId: item.ChatPushAppID,
		Status:        int(item.Status),
		Remark:        item.Remark,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}
