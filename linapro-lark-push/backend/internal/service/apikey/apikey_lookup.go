// 本文件实现签名鉴权路径与分组下拉所需的查询：按 app_key 取启用凭据、列出已配置的推送分组，
// 以及保存凭据时对分组与群聊推送应用的校验。

package apikey

import (
	"context"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-lark-push/backend/internal/dao"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
	"lina-plugin-linapro-lark-push/backend/internal/model/do"
	"lina-plugin-linapro-lark-push/backend/internal/model/entity"
)

// FindEnabledByAppKey 按 app_key 查询启用且未删除的凭据。
//
// 每次签名请求调用一次：命中部分唯一索引、只取鉴权所需的列，不做缓存（请求量低，换来停用 /
// 删除即时生效，且无需处理集群失效）。未命中返回 (nil, nil)，数据库失败返回错误。
func (s *serviceImpl) FindEnabledByAppKey(ctx context.Context, appKey string) (*Credential, error) {
	appKey = strings.TrimSpace(appKey)
	if appKey == "" {
		return nil, nil
	}
	cols := dao.ApiKey.Columns()
	var row *entity.ApiKey
	err := dao.ApiKey.Ctx(ctx).
		Fields(cols.Id, cols.AppKey, cols.Secret, cols.PushGroup, cols.ChatId, cols.ChatPushAppId).
		Where(do.ApiKey{AppKey: appKey, Status: int(StatusNormal)}).
		Scan(&row)
	if err != nil {
		return nil, gerror.Wrap(err, "apikey: 按 app_key 查询签名凭据失败")
	}
	if row == nil {
		return nil, nil
	}
	return &Credential{
		Id:            row.Id,
		AppKey:        row.AppKey,
		Secret:        row.Secret,
		Group:         row.PushGroup,
		ChatID:        row.ChatId,
		ChatPushAppID: row.ChatPushAppId,
	}, nil
}

// ListGroups 返回 push_groups 中已配置的分组（按名升序）及各自的推送应用（不含密钥）。
func (s *serviceImpl) ListGroups(ctx context.Context) ([]GroupOption, error) {
	if s.hostConfig == nil {
		return nil, bizerr.NewCode(CodeApiKeyGroupsUnavailable)
	}
	groups, err := larkcfg.Load(ctx, s.hostConfig)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiKeyGroupsUnavailable)
	}
	out := make([]GroupOption, 0, len(groups))
	for name, apps := range groups {
		option := GroupOption{Name: name, PushApps: make([]PushAppOption, 0, len(apps))}
		for _, app := range apps {
			option.PushApps = append(option.PushApps, PushAppOption{AppID: app.AppID, Name: app.Name})
		}
		out = append(out, option)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ensureGroupAndChat 确认分组在 push_groups 中已配置，并校验群聊推送应用：
//   - 配置了 chat_id 且分组有多个推送应用时，必须指定推送应用；
//   - 指定的推送应用必须属于该分组。
//
// 推送应用之后在配置中增删时，发送路径仍会按同一规则再校验一次（失败返回 400，不发送）。
func (s *serviceImpl) ensureGroupAndChat(ctx context.Context, in SaveInput) error {
	groups, err := s.ListGroups(ctx)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group.Name != in.Group {
			continue
		}
		if in.ChatID == "" {
			return nil
		}
		if in.ChatPushAppID == "" {
			if len(group.PushApps) > 1 {
				return invalidParam("分组 " + in.Group + " 有多个推送应用，配置群聊时须选择发送群聊的推送应用")
			}
			return nil
		}
		for _, app := range group.PushApps {
			if app.AppID == in.ChatPushAppID {
				return nil
			}
		}
		return invalidParam("群聊推送应用 " + in.ChatPushAppID + " 不属于分组 " + in.Group)
	}
	return bizerr.NewCode(CodeApiKeyGroupNotFound, bizerr.P("group", in.Group))
}
