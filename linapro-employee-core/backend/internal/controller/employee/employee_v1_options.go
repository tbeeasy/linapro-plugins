// employee_v1_options.go 实现两个下拉数据源：飞书应用清单与后台账号候选。
package employee

import (
	"context"

	"lina-core/pkg/plugin/capability/capmodel"
	"lina-core/pkg/plugin/capability/usercap"
	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
)

// ListLarkApps 返回本插件已配置的飞书应用（只给 app_id 与显示名，不含密钥）。
func (c *ControllerV1) ListLarkApps(ctx context.Context, _ *v1.ListLarkAppsReq) (res *v1.ListLarkAppsRes, err error) {
	res = &v1.ListLarkAppsRes{List: make([]*v1.LarkAppItem, 0)}
	if c.hostConfig == nil {
		return res, nil
	}
	apps, err := lark.LoadApps(ctx, c.hostConfig)
	if err != nil {
		return nil, err
	}
	names := c.larkAppNames(ctx)
	for _, app := range apps {
		res.List = append(res.List, &v1.LarkAppItem{LarkAppId: app.AppID, Name: names[app.AppID]})
	}
	return res, nil
}

// ListSysUserOptions 搜索后台账号候选。查询经宿主用户能力，结果受当前操作者的
// 用户数据权限约束；已关联其他员工的账号仍会返回，占用在保存时判定。
func (c *ControllerV1) ListSysUserOptions(ctx context.Context, req *v1.ListSysUserOptionsReq) (res *v1.ListSysUserOptionsRes, err error) {
	res = &v1.ListSysUserOptionsRes{List: make([]*v1.SysUserOption, 0)}
	if c.users == nil {
		// 宿主用户能力不可用时返回空候选，而不是放开成无过滤查询。
		return res, nil
	}
	pageNum, pageSize := req.PageNum, req.PageSize
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	out, err := c.users.List(ctx, usercap.ListInput{
		Keyword: req.Keyword,
		Page:    capmodel.PageRequest{PageNum: pageNum, PageSize: pageSize},
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return res, nil
	}
	res.Total = out.Total
	for _, u := range out.Items {
		if u == nil {
			continue
		}
		res.List = append(res.List, &v1.SysUserOption{
			Id:       userIDToInt(u.ID),
			Username: u.Username,
			Nickname: u.Nickname,
		})
	}
	return res, nil
}

// larkAppNames 读取配置中的飞书应用显示名（app_id → name）。读取失败或未配置时
// 返回空 map，调用方按「无显示名」降级。
func (c *ControllerV1) larkAppNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if c.hostConfig == nil {
		return out
	}
	apps, err := lark.LoadApps(ctx, c.hostConfig)
	if err != nil {
		return out
	}
	for _, app := range apps {
		if app.Name != "" {
			out[app.AppID] = app.Name
		}
	}
	return out
}
