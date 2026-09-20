// Package larkcontact 封装飞书（Lark）通讯录（Contact）OpenAPI 的二次调用：
// 全量枚举一个飞书企业本租户与关联组织的通讯录成员，作为共享库供多个插件复用。
//
// 枚举有两类渠道，统一用 User.Source 标识来源：
//   - own 渠道：遍历本租户通讯录。飞书 contact.user.list 只返回“指定部门的直属成员”
//     且不递归子部门，不指定部门则默认查根部门（"0"）直属成员——绝大多数企业员工
//     都挂在各级子部门下，故直接 list 返回空。因此本包先递归枚举全部部门（含根部门 0），
//     再逐部门 FindByDepartment 拉取直属成员，最后按 open_id 去重。
//   - assoc 渠道：遍历关联组织可见成员（trust_party.collaboration_tenant.list 列可见关联租户，
//     逐租户从根部门递归 collaboration_tenant.visible_organization 拉可见成员），返回的用户仅带 open_id、无 user_id。
//     该组接口频控为 1000次/分钟 & 50次/秒，远高于旧 directory/share_entities（100次/分钟）。
//
// own 与 assoc 结果按 open_id 去重后可合并（同一人在两个视角各有 open_id）。
package larkcontact

import (
	"context"
	"lina-core/pkg/logger"
	"linapro-lark-sdk/utils"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	contact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	trustparty "github.com/larksuite/oapi-sdk-go/v3/service/trust_party/v1"

	"linapro-lark-sdk/larkhttp"
)

// rootDepartmentID 是飞书通讯录根部门的固定 ID。根部门自身不在 Children
// 结果里，需显式纳入以覆盖挂在根下的直属成员。
const rootDepartmentID = "0"

// contactPageSize 是部门枚举与成员拉取的分页大小。
const contactPageSize = 50

// assocThrottle 是关联组织枚举中各请求之间的停顿。trust_party 接口频控为
// 1000次/分钟 & 50次/秒，此处 50ms 停顿把单 app 速率压到约 20次/秒，留足余量。
const assocThrottle = 50 * time.Millisecond

// assocUserDetailThrottle 是关联组织用户详情查询的速率限制。
// trust_party.collaboration_tenant_collaboration_user.get 接口限制为 5次/秒。
const assocUserDetailThrottle = 200 * time.Millisecond

// Source 标识成员来自哪个视角：own=本租户原生成员；assoc=关联组织视角成员。
const (
	// SourceOwn 是枚举本租户通讯录（contact.User.List / FindByDepartment）所得成员的来源。
	SourceOwn = "own"
	// SourceAssoc 是枚举关联组织（trust_party.collaboration_tenant.visible_organization）所得成员的来源。
	SourceAssoc = "assoc"
)

// User 是一条中立的飞书通讯录成员，剥离了 SDK 类型，供调用方按业务映射。
type User struct {
	// OpenID 是成员在该应用下的 open_id；跨应用不同。
	OpenID string
	// UserID 是成员的企业级 user_id；需应用具备”获取用户 user_id”权限才返回，
	// 缺权限时为空。own 通道返回本租户空间的 user_id；assoc 通道（Source=assoc）
	// 成员仅返回 open_id，不带 user_id（对方租户空间的 user_id 在当前共享租户链路下
	// 无任何消费方，故不采集、不落库，见 employee-core D17）。
	UserID string
	// Name 是成员在通讯录中的显示名；需应用具备”获取用户基本信息”权限才返回，
	// 缺权限时为空。
	Name string
	// EmployeeNo 是成员在通讯录中的工号（employee_no 扩展字段）；需应用具备
	// “获取用户雇佣信息”权限才返回，缺权限时为空。用于员工目录同步时按工号匹配。
	EmployeeNo string
	// Source 标识成员来源视角，取值为 SourceOwn 或 SourceAssoc。
	Source string
}

// Client 用一份 app 级凭证枚举单个飞书企业本租户的通讯录。
// 飞书 SDK 依据 appID/appSecret 自动管理 tenant access token。
type Client struct {
	appID  string
	client *larksdk.Client
}

// NewClient 依据飞书 app 凭证构造 Client。appID 会随日志与错误一起透出，
// 便于多企业场景下定位是哪个应用。
func NewClient(appID, appSecret string) *Client {
	return &Client{appID: appID, client: larksdk.NewClient(appID, appSecret, larksdk.WithHttpClient(larkhttp.NewClient()))}
}

// AppID 返回该 Client 对应的飞书 App ID。
func (c *Client) AppID() string { return c.appID }

// ListAllUsers 枚举该飞书企业的全部通讯录成员，并按 open_id 去重。
// open_id 或 name 为空的成员会被跳过（多为应用缺字段级读取权限所致），
// 并在 debug 日志中计数，便于排查授权范围/scope 问题。
//
// withAssoc 为 false 时仅枚举本租户（own 视角）成员；为 true 时追加关联组织
// （assoc 视角）共享成员，own 与 assoc 结果合并、按 open_id 去重。assoc 枚举失败
// soft-fail：记 warning 并跳过 assoc 通道，不阻断 own（返回的仍是 own 成员，非 error）。
// 任一步 own 枚举的 API 失败即返回 error，调用方可据此记错并跳过该应用。
func (c *Client) ListAllUsers(ctx context.Context, withAssoc bool) ([]User, error) {
	deptIDs, err := c.listDepartmentIDs(ctx)
	if err != nil {
		return nil, err
	}
	// 排障用：打印枚举到的部门 ID 全集。若只有 ["0"]，说明 Children 没返回任何
	// 子部门（多半是应用通讯录授权范围/scope 问题，而非代码逻辑）。
	logger.Debugf(ctx, "larkcontact: 部门列表 app=%s (%d) = %v", c.appID, len(deptIDs), deptIDs)
	seen := map[string]struct{}{}
	var users []User
	for _, deptID := range deptIDs {
		before := len(users)
		if err := c.appendDepartmentUsers(ctx, deptID, seen, &users); err != nil {
			return nil, err
		}
		// 排障用：打印该部门新增（去重后）成员数，便于定位是全公司空还是个别部门空。
		logger.Debugf(ctx, "larkcontact: 部门=%s app=%s 新增 %d 名成员", deptID, c.appID, len(users)-before)
	}
	if !withAssoc {
		return users, nil
	}
	assoc, err := c.listAssocUsers(ctx)
	if err != nil {
		// assoc 通道失败不阻断 own：记 warning 并降级为仅返回 own 用户。
		logger.Warningf(ctx, "larkcontact: 关联组织枚举 app=%s 软失败：%v", c.appID, err)
		return users, nil
	}
	for _, u := range assoc {
		if _, dup := seen[u.OpenID]; dup {
			continue
		}
		seen[u.OpenID] = struct{}{}
		users = append(users, u)
	}
	return users, nil
}

// listDepartmentIDs 递归枚举该企业中“有成员”的部门 ID，始终包含根部门 "0"。
// 借助 Children 接口的 fetch_child=true 一次性拉取某节点下的所有后代部门；
// 仅保留 member_count>0 的部门，member_count 为 0 的部门直接丢弃，省去对空
// 部门无谓的 FindByDepartment 调用。根部门自身不在 Children 结果里、拿不到
// member_count，故始终保留并照常查询一次（覆盖挂在根下的直属成员）。
func (c *Client) listDepartmentIDs(ctx context.Context) ([]string, error) {
	ids := []string{rootDepartmentID}
	pageToken := ""
	for {
		req := contact.NewChildrenDepartmentReqBuilder().
			DepartmentId(rootDepartmentID).
			DepartmentIdType("department_id").
			UserIdType("open_id").
			FetchChild(true).
			PageSize(contactPageSize)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		resp, err := c.client.Contact.Department.Children(ctx, req.Build())
		if err != nil {
			return nil, gerror.Wrapf(err, "larkcontact: 查询部门子节点失败 app=%s", c.appID)
		}
		if !resp.Success() {
			return nil, gerror.Newf("larkcontact: 查询部门子节点失败 app=%s req_id=%s code=%d msg=%s%s", c.appID, resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		if resp.Data == nil {
			break
		}
		for _, d := range resp.Data.Items {
			if d == nil || d.DepartmentId == nil {
				continue
			}
			// 仅保留有成员的部门；member_count 缺省或为 0 的部门跳过，
			// 省去后续对空部门的 FindByDepartment 调用。
			if d.MemberCount == nil || *d.MemberCount <= 0 {
				continue
			}
			ids = append(ids, *d.DepartmentId)
		}
		if resp.Data.HasMore == nil || !*resp.Data.HasMore || resp.Data.PageToken == nil {
			break
		}
		pageToken = *resp.Data.PageToken
	}
	return ids, nil
}

// appendDepartmentUsers 分页拉取单个部门的直属成员，按 open_id 去重后追加到 users。
// seen 跨部门共享，避免同一人隶属多部门时重复入列。
func (c *Client) appendDepartmentUsers(ctx context.Context, deptID string, seen map[string]struct{}, users *[]User) error {
	pageToken := ""
	for {
		req := contact.NewFindByDepartmentUserReqBuilder().
			DepartmentId(deptID).
			UserIdType("open_id").
			DepartmentIdType("department_id").
			PageSize(contactPageSize)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		resp, err := c.client.Contact.User.FindByDepartment(ctx, req.Build())
		if err != nil {
			return gerror.Wrapf(err, "larkcontact: 查询部门成员失败 app=%s 部门=%s", c.appID, deptID)
		}
		if !resp.Success() {
			return gerror.Newf("larkcontact: 查询部门成员失败 app=%s 部门=%s req_id=%s code=%d msg=%s%s", c.appID, deptID, resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		if resp.Data == nil {
			break
		}
		// 排障用：区分”接口没返回人”与”返回了但 open_id/name 为空被过滤”。
		// 后者说明应用缺字段级读取权限（scope），需去开放平台补授权。
		logger.Debugf(ctx, "larkcontact: 按部门查询成员 app=%s 部门=%s 原始条数=%d", c.appID, deptID, len(resp.Data.Items))
		skipped := 0
		for _, u := range resp.Data.Items {
			if u == nil || u.OpenId == nil || u.Name == nil {
				skipped++
				continue
			}
			if _, dup := seen[*u.OpenId]; dup {
				continue
			}
			seen[*u.OpenId] = struct{}{}
			usr := User{
				Name:   *u.Name,
				OpenID: *u.OpenId,
				Source: SourceOwn,
			}
			// user_id 需应用具备”获取用户 user_id”权限才返回；缺权限时留空。
			if u.UserId != nil {
				usr.UserID = *u.UserId
			}
			// employee_no 需应用具备”获取用户雇佣信息”权限才返回；缺权限时留空。
			if u.EmployeeNo != nil {
				usr.EmployeeNo = *u.EmployeeNo
			}
			*users = append(*users, usr)
		}
		if skipped > 0 {
			// 有成员但 open_id/name 被清空，几乎可以坐实是字段级读取权限缺失。
			logger.Debugf(ctx, "larkcontact: 按部门查询成员 app=%s 部门=%s 跳过 %d 条 open_id/name 为空的记录（大概率缺少字段读取权限）", c.appID, deptID, skipped)
		}
		if resp.Data.HasMore == nil || !*resp.Data.HasMore || resp.Data.PageToken == nil {
			break
		}
		pageToken = *resp.Data.PageToken
	}
	return nil
}

// listAssocUsers 枚举关联组织（assoc 视角）的可见成员，返回 Source="assoc" 的用户。
// 飞书关联组织是一对多的：先 trust_party.collaboration_tenant.list 列全部可见关联租户，
// 再逐租户从根部门（"0"）递归 trust_party.collaboration_tenant.visible_organization
// （TargetTenantKey 定位对方租户，按 TargetDepartmentId 逐级下钻可见部门）拉可见成员。
// 返回的用户仅带 open_id（无 user_id），并按 open_id 去重。open_id 或 name 为空的成员
// 被跳过并在 debug 日志计数。任一步 API 失败返回 error，由 ListAllUsers 按 soft-fail 处理。
func (c *Client) listAssocUsers(ctx context.Context) ([]User, error) {
	var users []User
	seen := map[string]struct{}{}

	tenantKeys, err := c.listCollaborationTenants(ctx)
	if err != nil {
		return nil, err
	}
	for _, tk := range tenantKeys {
		// 从对方租户根部门 "0" 起递归下钻可见组织。
		if err := c.appendVisibleOrg(ctx, tk, rootDepartmentID, "", seen, &users); err != nil {
			return nil, err
		}
	}
	return users, nil
}

// listCollaborationTenants 列举该应用可见的关联组织对方租户 tenant_key 列表。
// tenant_key 是后续 visible_organization 查询标识对方租户所需的键。
func (c *Client) listCollaborationTenants(ctx context.Context) ([]string, error) {
	var out []string
	pageToken := ""
	for {
		builder := trustparty.NewListCollaborationTenantReqBuilder().PageSize(contactPageSize)
		if pageToken != "" {
			builder = builder.PageToken(pageToken)
		}
		resp, err := c.client.TrustParty.V1.CollaborationTenant.List(ctx, builder.Build())
		if err != nil {
			return nil, gerror.Wrapf(err, "larkcontact: 查询关联租户失败 app=%s", c.appID)
		}
		if !resp.Success() {
			return nil, gerror.Newf("larkcontact: 查询关联租户失败 app=%s req_id=%s code=%d msg=%s%s", c.appID, resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		if resp.Data == nil {
			break
		}
		total := len(out)
		for _, t := range resp.Data.TargetTenantList {
			if t == nil || t.TenantKey == nil || *t.TenantKey == "" {
				continue
			}
			out = append(out, *t.TenantKey)
		}
		logger.Debugf(ctx, "larkcontact: 关联租户 app=%s 本页新增 %d（总计=%d）", c.appID, len(out)-total, len(out))
		time.Sleep(assocThrottle)
		if resp.Data.HasMore == nil || !*resp.Data.HasMore || resp.Data.PageToken == nil {
			break
		}
		pageToken = *resp.Data.PageToken
	}
	return out, nil
}

// appendVisibleOrg 拉取某个可见组织节点（对方租户的某个部门或用户组）下可见的
// 部门、成员、用户组，并递归下钻子部门/用户组。deptID 或 groupID 二选一非空标识
// 查询入口：deptID="0" 表示对方租户根部门。ID 一律按 open 形式传入（department_id_type
// / group_id_type 固定 open_*）。可见成员经 appendCollabUser 按 open_id 去重后追加进 users。
func (c *Client) appendVisibleOrg(ctx context.Context, tenantKey, deptID, groupID string, seen map[string]struct{}, users *[]User) error {
	pageToken := ""
	var childDepts, childGroups []string
	for {
		builder := trustparty.NewVisibleOrganizationCollaborationTenantReqBuilder().
			TargetTenantKey(tenantKey).
			PageSize(contactPageSize)
		if deptID != "" {
			builder = builder.DepartmentIdType("open_department_id").TargetDepartmentId(deptID)
		}
		if groupID != "" {
			builder = builder.GroupIdType("open_group_id").TargetGroupId(groupID)
		}
		if pageToken != "" {
			builder = builder.PageToken(pageToken)
		}
		// 每次请求前停顿，把单 app 递归速率压到约 20次/秒，远低于 50次/秒 上限。
		time.Sleep(assocThrottle)
		resp, err := c.client.TrustParty.V1.CollaborationTenant.VisibleOrganization(ctx, builder.Build())
		if err != nil {
			return gerror.Wrapf(err, "larkcontact: 查询可见组织失败 app=%s 租户=%s 部门=%s 用户组=%s", c.appID, tenantKey, deptID, groupID)
		}
		if !resp.Success() {
			return gerror.Newf("larkcontact: 查询可见组织失败 app=%s 租户=%s 部门=%s 用户组=%s req_id=%s code=%d msg=%s%s", c.appID, tenantKey, deptID, groupID, resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
		}
		if resp.Data == nil {
			break
		}
		skipped := 0
		for _, e := range resp.Data.CollaborationEntityList {
			if e == nil {
				continue
			}
			switch {
			// 成员：追加进结果（按 open_id 去重）。
			case e.OpenUserId != nil && *e.OpenUserId != "":
				if !c.appendCollabUser(ctx, tenantKey, e, seen, users) {
					skipped++
				}
			// 子部门：登记待递归下钻。
			case e.OpenDepartmentId != nil && *e.OpenDepartmentId != "":
				childDepts = append(childDepts, *e.OpenDepartmentId)
			// 用户组：登记待递归下钻。
			case e.OpenGroupId != nil && *e.OpenGroupId != "":
				childGroups = append(childGroups, *e.OpenGroupId)
			}
		}
		logger.Debugf(ctx, "larkcontact: 可见组织 app=%s 租户=%s 部门=%s 用户组=%s 实体数=%d 子部门=%d 子用户组=%d", c.appID, tenantKey, deptID, groupID, len(resp.Data.CollaborationEntityList), len(childDepts), len(childGroups))
		if skipped > 0 {
			logger.Debugf(ctx, "larkcontact: 可见组织 app=%s 租户=%s 部门=%s 用户组=%s 跳过 %d 名关联成员（open_id/name 为空）", c.appID, tenantKey, deptID, groupID, skipped)
		}
		if resp.Data.HasMore == nil || !*resp.Data.HasMore || resp.Data.PageToken == nil {
			break
		}
		pageToken = *resp.Data.PageToken
	}
	for _, d := range childDepts {
		if err := c.appendVisibleOrg(ctx, tenantKey, d, "", seen, users); err != nil {
			return err
		}
	}
	for _, grp := range childGroups {
		if err := c.appendVisibleOrg(ctx, tenantKey, "", grp, seen, users); err != nil {
			return err
		}
	}
	return nil
}

// appendCollabUser 把一条关联组织成员追加进 users（按 open_id 去重）。缺少 open_id 或
// name 时返回 false（调用方计数提示 scope 缺失）；成功追加或重复返回 true。
// 调用关联组织用户详情接口获取工号（频控 5次/秒，此处 200ms 停顿）。
func (c *Client) appendCollabUser(ctx context.Context, tenantKey string, e *trustparty.CollaborationEntity, seen map[string]struct{}, users *[]User) bool {
	if e == nil || e.OpenUserId == nil || *e.OpenUserId == "" {
		return false
	}
	name := collabUserName(e)
	if name == "" {
		return false
	}
	if _, dup := seen[*e.OpenUserId]; dup {
		return true
	}
	seen[*e.OpenUserId] = struct{}{}

	// 调用详情接口获取工号
	var employeeNo string
	time.Sleep(assocUserDetailThrottle) // 速率限制：5次/秒
	req := trustparty.NewGetCollaborationTenantCollaborationUserReqBuilder().
		TargetTenantKey(tenantKey).
		TargetUserId(*e.OpenUserId).
		TargetUserIdType("open_id").
		Build()
	resp, err := c.client.TrustParty.V1.CollaborationTenantCollaborationUser.Get(ctx, req)
	if err != nil {
		logger.Warningf(ctx, "larkcontact: 获取关联成员详情失败 app=%s 租户=%s open_id=%s: %v", c.appID, tenantKey, *e.OpenUserId, err)
	}

	if !resp.Success() {
		logger.Errorf(ctx, "larkcontact: 获取关联成员详情失败 app=%s 租户=%s open_id=%s req_id=%s code=%d msg=%s%s", c.appID, tenantKey, *e.OpenUserId, resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
	}

	if resp != nil && resp.Data != nil && resp.Data.TargetUser != nil && resp.Data.TargetUser.EmployeeNo != nil {
		employeeNo = *resp.Data.TargetUser.EmployeeNo
	}

	*users = append(*users, User{
		OpenID:     *e.OpenUserId,
		Name:       name,
		EmployeeNo: employeeNo,
		Source:     SourceAssoc,
	})
	return true
}

// collabUserName 从关联组织成员实体提取可展示成员名：优先 user_name，
// 其次按 zh_cn → en_us → ja_jp 顺序取 i18n_user_name。
func collabUserName(e *trustparty.CollaborationEntity) string {
	if e.UserName != nil && *e.UserName != "" {
		return *e.UserName
	}
	return i18nNameValue(e.I18nUserName)
}

// i18nNameValue 从 I18nName 提取一个可展示的字符串：按 zh_cn → en_us → ja_jp 顺序取值。
func i18nNameValue(n *trustparty.I18nName) string {
	if n == nil {
		return ""
	}
	if n.ZhCn != nil && *n.ZhCn != "" {
		return *n.ZhCn
	}
	if n.EnUs != nil && *n.EnUs != "" {
		return *n.EnUs
	}
	if n.JaJp != nil && *n.JaJp != "" {
		return *n.JaJp
	}
	return ""
}
