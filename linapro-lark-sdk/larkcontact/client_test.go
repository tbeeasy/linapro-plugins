package larkcontact

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// fakeHTTP 实现 larkcore.HttpClient（仅需 Do 方法），拦截飞书通讯录请求：
// 鉴权请求返回固定 token；departments/children 回放注入的部门（含 member_count）；
// users/find_by_department 按 department_id 回放该部门的成员；关联组织枚举
// 回放 trust_party.collaboration_tenant.list 与 collaboration_tenant.visible_organization。
type fakeHTTP struct {
	// depts 为 Children(fetch_child=true) 应返回的部门（不含根部门 0）。
	depts []deptJSON
	// usersByDept 按 department_id 索引该部门直属成员。
	usersByDept map[string][]userJSON
	// findCalls 记录每次 find_by_department 查询的 department_id（按调用顺序）。
	findCalls []string

	// collabTenants 为 collaboration_tenant.list 应返回的关联租户 tenant_key 列表。
	collabTenants []string
	// shareByNode 按节点键回放可见组织实体：键为 tenantKey/d<deptID>（部门节点，
	// 顶层为 tenantKey/d0）或 tenantKey/g<groupID>（用户组节点）。
	shareByNode map[string][]collabEntityJSON
	// collabFail 置 true 时协作（trust_party 关联组织）接口返回非零 code，
	// 用于测 assoc 枚举 soft-fail 不阻断 own。
	collabFail bool
	// collabTenantCalls 记录 collaboration_tenant.list 的调用次数。
	collabTenantCalls int
	// shareCalls 记录每次 visible_organization 的 target_tenant_key（按调用顺序）。
	shareCalls []string
}

type deptJSON struct {
	DepartmentID string `json:"department_id"`
	MemberCount  *int   `json:"member_count,omitempty"`
}

type userJSON struct {
	OpenID *string `json:"open_id,omitempty"`
	UserID *string `json:"user_id,omitempty"`
	Name   *string `json:"name,omitempty"`
}

// collabEntityJSON 是 visible_organization 返回的一条关联组织实体：成员填 open_user_id
// 与 user_name；子部门填 open_department_id；用户组填 open_group_id（供递归下钻）。
type collabEntityJSON struct {
	OpenUserID       *string `json:"open_user_id,omitempty"`
	UserName         *string `json:"user_name,omitempty"`
	OpenDepartmentID *string `json:"open_department_id,omitempty"`
	OpenGroupID      *string `json:"open_group_id,omitempty"`
}

func (f *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	path := req.URL.Path

	// 鉴权：返回固定 tenant_access_token。
	if strings.HasSuffix(path, "/auth/v3/tenant_access_token/internal") ||
		strings.HasSuffix(path, "/auth/v3/app_access_token/internal") {
		return jsonResp(`{"code":0,"msg":"ok","tenant_access_token":"t","app_access_token":"t","expire":7200}`), nil
	}

	// 部门递归枚举：一次性返回全部注入部门（不分页）。
	if strings.HasSuffix(path, "/children") {
		out := map[string]any{
			"code": 0, "msg": "success",
			"data": map[string]any{"items": f.depts, "has_more": false},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// 按部门拉成员：从 query 取 department_id，回放该部门成员。
	if strings.HasSuffix(path, "/users/find_by_department") {
		deptID := req.URL.Query().Get("department_id")
		f.findCalls = append(f.findCalls, deptID)
		out := map[string]any{
			"code": 0, "msg": "success",
			"data": map[string]any{"items": f.usersByDept[deptID], "has_more": false},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// 关联组织租户列表：回放全部 tenant_key。
	if strings.HasSuffix(path, "/trust_party/v1/collaboration_tenants") {
		f.collabTenantCalls++
		if f.collabFail {
			return jsonResp(`{"code":99991400,"msg":"rate limited"}`), nil
		}
		items := make([]map[string]any, 0, len(f.collabTenants))
		for _, tk := range f.collabTenants {
			items = append(items, map[string]any{"tenant_key": tk})
		}
		out := map[string]any{
			"code": 0, "msg": "success",
			"data": map[string]any{"target_tenant_list": items, "has_more": false},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// 关联组织可见成员：路径形如 /trust_party/v1/collaboration_tenants/:tenant/visible_organization，
	// 按 target_tenant_key(路径) + 下钻参数定位节点，回放可见实体（成员/子部门/用户组）。
	if strings.HasSuffix(path, "/visible_organization") {
		tenantKey := pathTenantKey(path)
		deptID := req.URL.Query().Get("target_department_id")
		groupID := req.URL.Query().Get("target_group_id")
		f.shareCalls = append(f.shareCalls, tenantKey)
		if f.collabFail {
			return jsonResp(`{"code":99991400,"msg":"rate limited"}`), nil
		}
		// 顶层查询以 dept="0" 入口；节点键统一为 tenantKey/d<deptID> 或 tenantKey/g<groupID>。
		node := tenantKey + "/d" + deptID
		if groupID != "" {
			node = tenantKey + "/g" + groupID
		}
		out := map[string]any{
			"code": 0, "msg": "success",
			"data": map[string]any{
				"collaboration_entity_list": f.shareByNode[node],
				"has_more":                  false,
			},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	return jsonResp(`{"code":0,"msg":"ok"}`), nil
}

// pathTenantKey 从 visible_organization 请求路径中提取 target_tenant_key 路径参数，
// 路径形如 /open-apis/trust_party/v1/collaboration_tenants/<tenant>/visible_organization。
func pathTenantKey(path string) string {
	const marker = "/collaboration_tenants/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	rest := path[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
}

// newTestClient 构造一个注入了 fake HttpClient 的 Client（同包可直接装配私有字段）。
func newTestClient(f *fakeHTTP) *Client {
	return &Client{appID: "id", client: lark.NewClient("id", "secret", lark.WithHttpClient(f))}
}

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }

// TestListAllUsers_TraversesDepartmentsAndDedups 验证：只查询 member_count>0 的部门
// （外加根部门 0），跨部门按 open_id 去重。
func TestListAllUsers_TraversesDepartmentsAndDedups(t *testing.T) {
	f := &fakeHTTP{
		depts: []deptJSON{
			{DepartmentID: "d1", MemberCount: intptr(2)},
			{DepartmentID: "d2", MemberCount: intptr(0)}, // 空部门：应被过滤
			{DepartmentID: "d3", MemberCount: intptr(1)},
		},
		usersByDept: map[string][]userJSON{
			"d1": {
				{OpenID: strptr("ou_a"), UserID: strptr("u_a"), Name: strptr("张三")},
				{OpenID: strptr("ou_b"), UserID: strptr("u_b"), Name: strptr("李四")},
			},
			// d3 与 d1 共享 ou_a（同一人隶属多部门）：去重后只出现一次。
			"d3": {
				{OpenID: strptr("ou_a"), UserID: strptr("u_a"), Name: strptr("张三")},
				{OpenID: strptr("ou_c"), UserID: strptr("u_c"), Name: strptr("王五")},
			},
		},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), false)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("去重后应为 3 人, got=%d (%+v)", len(users), users)
	}

	// 三个成员均来自 own 渠道。
	for _, u := range users {
		if u.Source != SourceOwn {
			t.Errorf("成员 %s 的 Source 应为 %q, got=%q", u.OpenID, SourceOwn, u.Source)
		}
	}

	// 查询的部门应为 [0, d1, d3]——d2 被 member_count=0 过滤掉。
	want := map[string]bool{"0": true, "d1": true, "d3": true}
	if len(f.findCalls) != 3 {
		t.Fatalf("应查询 3 个部门, 实际 %d: %v", len(f.findCalls), f.findCalls)
	}
	for _, id := range f.findCalls {
		if !want[id] {
			t.Errorf("查询了非预期部门 %q (member_count=0 的 d2 不应被查询)", id)
		}
	}
}

// TestListAllUsers_SkipsNilFields 验证 open_id 或 name 为空的成员被跳过
// （模拟应用缺字段级读取权限）。
func TestListAllUsers_SkipsNilFields(t *testing.T) {
	f := &fakeHTTP{
		depts: []deptJSON{{DepartmentID: "d1", MemberCount: intptr(3)}},
		usersByDept: map[string][]userJSON{
			"d1": {
				{OpenID: strptr("ou_a"), Name: strptr("张三")}, // 正常
				{OpenID: strptr("ou_b"), Name: nil},          // name 空：缺基本信息权限
				{OpenID: nil, Name: strptr("王五")},            // open_id 空：缺 ID 权限
			},
		},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), false)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("仅 1 个完整成员应入列, got=%d (%+v)", len(users), users)
	}
	if users[0].OpenID != "ou_a" || users[0].Name != "张三" {
		t.Errorf("入列成员不符: %+v", users[0])
	}
}

// TestListAllUsers_WithAssocReturnsAssocIdentity 验证开启 withAssoc 时关联组织枚举：
// 列关联租户后逐租户拉共享成员，返回 Source="assoc"、仅带 open_id（无 user_id）。
func TestListAllUsers_WithAssocReturnsAssocIdentity(t *testing.T) {
	f := &fakeHTTP{
		// own 渠道返回空（根部门直属 0 人，无子部门）：仅验证 assoc 通道身份。
		depts: nil,
		usersByDept: map[string][]userJSON{
			"0": nil,
		},
		collabTenants: []string{"tk_b", "tk_c"},
		shareByNode: map[string][]collabEntityJSON{
			// 租户 b 根部门：可见成员小王（ou_b1）、小李（ou_b2）。
			"tk_b/d0": {
				{OpenUserID: strptr("ou_b1"), UserName: strptr("小王")},
				{OpenUserID: strptr("ou_b2"), UserName: strptr("小李")},
			},
			// 租户 c 根部门：可见成员小赵（ou_c1）。
			"tk_c/d0": {
				{OpenUserID: strptr("ou_c1"), UserName: strptr("小赵")},
			},
		},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("应为 3 个 assoc 成员, got=%d (%+v)", len(users), users)
	}
	// 每个成员均 Source=assoc、无 UserID。
	for _, u := range users {
		if u.Source != SourceAssoc {
			t.Errorf("成员 %s 的 Source 应为 %q, got=%q", u.OpenID, SourceAssoc, u.Source)
		}
		if u.UserID != "" {
			t.Errorf("assoc 成员 %s 不该有 user_id, got=%q", u.OpenID, u.UserID)
		}
	}
	// 应分别查过 tk_b 与 tk_c。
	if len(f.shareCalls) != 2 {
		t.Fatalf("应查 2 个关联租户, 实际 %d: %v", len(f.shareCalls), f.shareCalls)
	}
	if f.collabTenantCalls != 1 {
		t.Fatalf("collaboration_tenant.list 应只查 1 次, 实际 %d", f.collabTenantCalls)
	}
}

// TestListAllUsers_WithAssocMergesOwnAndAssoc 验证 withAssoc=true 时 own 与 assoc 合并，
// 同一 open_id 在两视角同时出现时在结果中去重。
func TestListAllUsers_WithAssocMergesOwnAndAssoc(t *testing.T) {
	f := &fakeHTTP{
		depts: []deptJSON{{DepartmentID: "d1", MemberCount: intptr(2)}},
		usersByDept: map[string][]userJSON{
			"d1": {
				// ou_own 是 own 租户成员；留一个 ou_own 也与 assoc 共享（同名同 id 空间不同）。
				{OpenID: strptr("ou_own"), UserID: strptr("u_o"), Name: strptr("张三")},
				{OpenID: strptr("ou_own2"), UserID: strptr("u_o2"), Name: strptr("李四")},
			},
		},
		collabTenants: []string{"tk_b"},
		shareByNode: map[string][]collabEntityJSON{
			"tk_b/d0": {
				// 与 own 的 ou_own 不同 id 空间：assoc 视角另有 open_id，不重复。
				{OpenUserID: strptr("ou_own"), UserName: strptr("张三")},
				{OpenUserID: strptr("ou_assoc"), UserName: strptr("王五")},
			},
		},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	// own 2 人 + assoc 1 人（ou_own 已被 own 去重，ou_assoc 新增）＝3 人。
	if len(users) != 3 {
		t.Fatalf("合并去重后应为 3 人, got=%d (%+v)", len(users), users)
	}
	byID := map[string]User{}
	for _, u := range users {
		byID[u.OpenID] = u
	}
	if byID["ou_own"].Source != SourceOwn {
		t.Errorf("ou_own 应来自 own 渠道, got=%q", byID["ou_own"].Source)
	}
	if byID["ou_assoc"].Source != SourceAssoc {
		t.Errorf("ou_assoc 应来自 assoc 渠道, got=%q", byID["ou_assoc"].Source)
	}
}

// TestListAllUsers_WithoutAssocSkipsAssocChannel 验证 withAssoc=false（默认）时不发起
// 任何关联组织请求，仅返回 own 成员。
func TestListAllUsers_WithoutAssocSkipsAssocChannel(t *testing.T) {
	f := &fakeHTTP{
		depts: []deptJSON{{DepartmentID: "d1", MemberCount: intptr(1)}},
		usersByDept: map[string][]userJSON{
			"d1": {{OpenID: strptr("ou_own"), Name: strptr("张三")}},
		},
		collabTenants: []string{"tk_b"},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), false)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	if len(users) != 1 || users[0].Source != SourceOwn {
		t.Fatalf("关闭时仅应返回 own 成员, got=%d (%+v)", len(users), users)
	}
	if f.collabTenantCalls != 0 {
		t.Fatalf("关闭时不应发起关联组织请求, 实际 %d 次", f.collabTenantCalls)
	}
}

// TestListAllUsers_WithAssocSoftFailDoesNotBlockOwn 验证 assoc 枚举失败（缺 scope /
// 限频）时 soft-fail：记 warning 并跳过 assoc，own 通道正常返回。
func TestListAllUsers_WithAssocSoftFailDoesNotBlockOwn(t *testing.T) {
	f := &fakeHTTP{
		depts: []deptJSON{{DepartmentID: "d1", MemberCount: intptr(1)}},
		usersByDept: map[string][]userJSON{
			"d1": {{OpenID: strptr("ou_own"), Name: strptr("张三")}},
		},
		collabTenants: []string{"tk_b"},
		collabFail:    true, // 关联组织接口返回非零 code
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("soft-fail 不应把整体枚举判为失败: %v", err)
	}
	if len(users) != 1 || users[0].OpenID != "ou_own" || users[0].Source != SourceOwn {
		t.Fatalf("assoc 失败后应仅返回 own 成员, got=%d (%+v)", len(users), users)
	}
}

// TestListAllUsers_WithAssocRecursesDeptsAndGroups 验证 visible_organization 的递归下钻：
// 根部门返回成员 + 子部门 + 用户组，逐级下钻拉全部可见成员，并按 open_id 去重。
func TestListAllUsers_WithAssocRecursesDeptsAndGroups(t *testing.T) {
	f := &fakeHTTP{
		// own 渠道空，仅验证 assoc 递归。
		usersByDept:   map[string][]userJSON{"0": nil},
		collabTenants: []string{"tk_b"},
		shareByNode: map[string][]collabEntityJSON{
			// 根部门：成员 ou_root + 子部门 od_sub + 用户组 og_grp。
			"tk_b/d0": {
				{OpenUserID: strptr("ou_root"), UserName: strptr("根成员")},
				{OpenDepartmentID: strptr("od_sub")},
				{OpenGroupID: strptr("og_grp")},
			},
			// 子部门：成员 ou_sub（另有一个与根重复的 ou_root，去重后不重复计入）。
			"tk_b/dod_sub": {
				{OpenUserID: strptr("ou_sub"), UserName: strptr("子部门成员")},
				{OpenUserID: strptr("ou_root"), UserName: strptr("根成员")},
			},
			// 用户组：成员 ou_grp。
			"tk_b/gog_grp": {
				{OpenUserID: strptr("ou_grp"), UserName: strptr("用户组成员")},
			},
		},
	}
	c := newTestClient(f)

	users, err := c.ListAllUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListAllUsers: %v", err)
	}
	// 去重后应为 ou_root、ou_sub、ou_grp 共 3 人。
	if len(users) != 3 {
		t.Fatalf("递归去重后应为 3 人, got=%d (%+v)", len(users), users)
	}
	got := map[string]bool{}
	for _, u := range users {
		if u.Source != SourceAssoc {
			t.Errorf("成员 %s 的 Source 应为 %q, got=%q", u.OpenID, SourceAssoc, u.Source)
		}
		got[u.OpenID] = true
	}
	for _, want := range []string{"ou_root", "ou_sub", "ou_grp"} {
		if !got[want] {
			t.Errorf("缺少期望成员 %s", want)
		}
	}
	// 应发起 3 次 visible_organization：根部门、子部门、用户组。
	if len(f.shareCalls) != 3 {
		t.Fatalf("应发起 3 次 visible_organization, 实际 %d: %v", len(f.shareCalls), f.shareCalls)
	}
}
