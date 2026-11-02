// 本文件实现签名凭据的后台管理：分页列表、详情、新增、编辑、启停、软删除与查看 secret，
// 以及保存前的参数规范化与校验。所有查询都只读当前接口需要的列，列表与详情不读 secret。

package apikey

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"lina-core/pkg/apitime"
	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-lark-push/backend/internal/dao"
	"lina-plugin-linapro-lark-push/backend/internal/model/do"
	"lina-plugin-linapro-lark-push/backend/internal/model/entity"
)

// 分页与字段长度约束。
const (
	defaultPageNum  = 1
	defaultPageSize = 10
	maxPageSize     = 100

	maxNameRunes   = 64
	maxRemarkRunes = 500
	minSecretBytes = 16
	maxSecretBytes = 128
)

// chatIDPattern 限定 chat_id 形态：飞书群聊 ID 以 oc_ 开头，只含字母与数字。保存时校验格式，
// 能否送达（机器人是否在群内）只有发送时才知道，由投递明细暴露。
var chatIDPattern = regexp.MustCompile(`^oc_[A-Za-z0-9]{1,120}$`)

// appKeyPattern 限定 app_key 字符集与长度：它会进请求头、日志与 nonce 缓存键，
// 只允许不需要转义的字符，长度与签名中间件的 MaxAppKeyBytes 一致。
var appKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,64}$`)

// List 分页查询凭据（不含 secret），按更新时间倒序。
func (s *serviceImpl) List(ctx context.Context, in ListInput) (*ListOutput, error) {
	cols := dao.ApiKey.Columns()
	model := dao.ApiKey.Ctx(ctx)
	if kw := strings.TrimSpace(in.Keyword); kw != "" {
		like := "%" + kw + "%"
		model = model.Where(model.Builder().WhereLike(cols.Name, like).WhereOrLike(cols.AppKey, like))
	}
	if group := strings.TrimSpace(in.Group); group != "" {
		model = model.Where(do.ApiKey{PushGroup: group})
	}
	if in.Status != nil {
		model = model.Where(do.ApiKey{Status: int(*in.Status)})
	}

	total, err := model.Count()
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	pageNum, pageSize := normalizePage(in.PageNum, in.PageSize)
	rows := make([]*entity.ApiKey, 0, pageSize)
	err = model.
		Fields(publicColumns()...).
		OrderDesc(cols.UpdatedAt).
		OrderDesc(cols.Id).
		Page(pageNum, pageSize).
		Scan(&rows)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	out := &ListOutput{List: make([]*Item, 0, len(rows)), Total: total}
	for _, row := range rows {
		out.List = append(out.List, toItem(row))
	}
	return out, nil
}

// Get 返回单条凭据（不含 secret）。
func (s *serviceImpl) Get(ctx context.Context, id int64) (*Item, error) {
	var row *entity.ApiKey
	err := dao.ApiKey.Ctx(ctx).Fields(publicColumns()...).Where(do.ApiKey{Id: id}).Scan(&row)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	if row == nil {
		return nil, bizerr.NewCode(CodeApiKeyNotFound)
	}
	return toItem(row), nil
}

// Create 新增凭据；secret 必填，服务端不代为生成（管理页打开新增表单时已在浏览器预填随机值）。
func (s *serviceImpl) Create(ctx context.Context, in SaveInput) (*CreateOutput, error) {
	in, err := normalizeSaveInput(in)
	if err != nil {
		return nil, err
	}
	// 空 secret 只在编辑时表示「保持原值」，新增时必须给出；先于查库校验，非法请求不访问数据库。
	if in.Secret == "" {
		return nil, invalidParam("新增凭据时 secret 不能为空")
	}
	if err = s.ensureGroupAndChat(ctx, in); err != nil {
		return nil, err
	}
	if err = ensureAppKeyAvailable(ctx, in.AppKey, 0); err != nil {
		return nil, err
	}
	id, err := dao.ApiKey.Ctx(ctx).Data(do.ApiKey{
		Name:          in.Name,
		AppKey:        in.AppKey,
		Secret:        in.Secret,
		PushGroup:     in.Group,
		ChatId:        in.ChatID,
		ChatPushAppId: in.ChatPushAppID,
		Status:        int(in.Status),
		Remark:        in.Remark,
	}).InsertAndGetId()
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiKeySaveFailed)
	}
	return &CreateOutput{Id: id, Secret: in.Secret}, nil
}

// Update 编辑凭据；secret 为空时保持原值。
func (s *serviceImpl) Update(ctx context.Context, id int64, in SaveInput) error {
	in, err := normalizeSaveInput(in)
	if err != nil {
		return err
	}
	if err = ensureExists(ctx, id); err != nil {
		return err
	}
	if err = s.ensureGroupAndChat(ctx, in); err != nil {
		return err
	}
	if err = ensureAppKeyAvailable(ctx, in.AppKey, id); err != nil {
		return err
	}
	data := do.ApiKey{
		Name:          in.Name,
		AppKey:        in.AppKey,
		PushGroup:     in.Group,
		ChatId:        in.ChatID,
		ChatPushAppId: in.ChatPushAppID,
		Status:        int(in.Status),
		Remark:        in.Remark,
	}
	if in.Secret != "" {
		data.Secret = in.Secret
	}
	if _, err = dao.ApiKey.Ctx(ctx).Data(data).Where(do.ApiKey{Id: id}).Update(); err != nil {
		return bizerr.WrapCode(err, CodeApiKeySaveFailed)
	}
	return nil
}

// SetStatus 启用或停用凭据。
func (s *serviceImpl) SetStatus(ctx context.Context, id int64, status Status) error {
	if err := validateStatus(status); err != nil {
		return err
	}
	if err := ensureExists(ctx, id); err != nil {
		return err
	}
	if _, err := dao.ApiKey.Ctx(ctx).Data(do.ApiKey{Status: int(status)}).Where(do.ApiKey{Id: id}).Update(); err != nil {
		return bizerr.WrapCode(err, CodeApiKeySaveFailed)
	}
	return nil
}

// Delete 软删除凭据（表含 deleted_at，Delete 由 ORM 自动转为软删除）。
func (s *serviceImpl) Delete(ctx context.Context, id int64) error {
	if err := ensureExists(ctx, id); err != nil {
		return err
	}
	if _, err := dao.ApiKey.Ctx(ctx).Where(do.ApiKey{Id: id}).Delete(); err != nil {
		return bizerr.WrapCode(err, CodeApiKeyDeleteFailed)
	}
	return nil
}

// GetSecret 返回凭据 secret 明文。
func (s *serviceImpl) GetSecret(ctx context.Context, id int64) (string, error) {
	value, err := dao.ApiKey.Ctx(ctx).Fields(dao.ApiKey.Columns().Secret).Where(do.ApiKey{Id: id}).Value()
	if err != nil {
		return "", bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	if value == nil || value.IsNil() {
		return "", bizerr.NewCode(CodeApiKeyNotFound)
	}
	return value.String(), nil
}

// publicColumns 返回列表与详情需要的列（不含 secret 与 deleted_at）。
func publicColumns() []any {
	cols := dao.ApiKey.Columns()
	return []any{
		cols.Id, cols.Name, cols.AppKey, cols.PushGroup, cols.ChatId, cols.ChatPushAppId,
		cols.Status, cols.Remark, cols.CreatedAt, cols.UpdatedAt,
	}
}

// toItem 把实体投影为公开视图。
func toItem(row *entity.ApiKey) *Item {
	return &Item{
		Id:            row.Id,
		Name:          row.Name,
		AppKey:        row.AppKey,
		Group:         row.PushGroup,
		ChatID:        row.ChatId,
		ChatPushAppID: row.ChatPushAppId,
		Status:        Status(row.Status),
		Remark:        row.Remark,
		CreatedAt:     apitime.Milli(row.CreatedAt),
		UpdatedAt:     apitime.Milli(row.UpdatedAt),
	}
}

// normalizePage 规范化分页参数。
func normalizePage(pageNum, pageSize int) (int, int) {
	if pageNum < 1 {
		pageNum = defaultPageNum
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageNum, pageSize
}

// normalizeSaveInput 去首尾空白并校验字段。空 secret 在这里放行：编辑时表示保持原值，新增时由
// Create 拒绝；非空 secret 必须满足长度与字符集要求。
func normalizeSaveInput(in SaveInput) (SaveInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.AppKey = strings.TrimSpace(in.AppKey)
	in.Secret = strings.TrimSpace(in.Secret)
	in.Group = strings.TrimSpace(in.Group)
	in.ChatID = strings.TrimSpace(in.ChatID)
	in.ChatPushAppID = strings.TrimSpace(in.ChatPushAppID)
	in.Remark = strings.TrimSpace(in.Remark)

	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > maxNameRunes:
		return in, invalidParam("名称不能为空且不超过 64 个字符")
	case !appKeyPattern.MatchString(in.AppKey):
		return in, invalidParam("app_key 须为 1~64 个字母、数字、_、-、. 字符")
	case in.Group == "":
		return in, invalidParam("分组不能为空")
	case in.ChatID != "" && !chatIDPattern.MatchString(in.ChatID):
		return in, invalidParam("群聊 chat_id 须以 oc_ 开头，仅含字母与数字")
	case in.ChatID == "" && in.ChatPushAppID != "":
		return in, invalidParam("未配置群聊 chat_id 时不能指定群聊推送应用")
	case utf8.RuneCountInString(in.Remark) > maxRemarkRunes:
		return in, invalidParam("备注不超过 500 个字符")
	}
	if err := validateStatus(in.Status); err != nil {
		return in, err
	}
	if in.Secret != "" && !validSecret(in.Secret) {
		return in, invalidParam("secret 须为 16~128 个可见 ASCII 字符")
	}
	return in, nil
}

// validateStatus 校验状态取值。
func validateStatus(status Status) error {
	if status != StatusNormal && status != StatusDisabled {
		return invalidParam("状态只能为 1（正常）或 0（停用）")
	}
	return nil
}

// validSecret 报告 secret 是否为 16~128 个可见 ASCII 字符（不含空格）。
// 限定可见 ASCII 是为了让各语言客户端按 UTF-8 / 字节取 key 时结果一致。
func validSecret(secret string) bool {
	if len(secret) < minSecretBytes || len(secret) > maxSecretBytes {
		return false
	}
	for i := 0; i < len(secret); i++ {
		if secret[i] < 0x21 || secret[i] > 0x7e {
			return false
		}
	}
	return true
}

// ensureExists 确认凭据存在且未删除。
func ensureExists(ctx context.Context, id int64) error {
	n, err := dao.ApiKey.Ctx(ctx).Where(do.ApiKey{Id: id}).Count()
	if err != nil {
		return bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	if n == 0 {
		return bizerr.NewCode(CodeApiKeyNotFound)
	}
	return nil
}

// ensureAppKeyAvailable 确认 app_key 未被其它未删除凭据占用；selfID 为编辑中的凭据 ID（新增传 0）。
// 并发写入的极端情况由部分唯一索引兜底，届时保存返回 CodeApiKeySaveFailed。
func ensureAppKeyAvailable(ctx context.Context, appKey string, selfID int64) error {
	cols := dao.ApiKey.Columns()
	model := dao.ApiKey.Ctx(ctx).Where(do.ApiKey{AppKey: appKey})
	if selfID > 0 {
		model = model.WhereNot(cols.Id, selfID)
	}
	n, err := model.Count()
	if err != nil {
		return bizerr.WrapCode(err, CodeApiKeyQueryFailed)
	}
	if n > 0 {
		return bizerr.NewCode(CodeApiKeyAppKeyExists, bizerr.P("appKey", appKey))
	}
	return nil
}

// invalidParam 构造参数错误。
func invalidParam(reason string) error {
	return bizerr.NewCode(CodeApiKeyInvalidParam, bizerr.P("item", reason))
}
