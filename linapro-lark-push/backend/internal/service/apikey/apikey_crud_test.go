// apikey_crud_test.go 覆盖 apikey_crud.go：新增（secret 必填）、app_key 唯一、分组校验、
// 列表不含 secret 与过滤、编辑保持 secret、启停、软删除后重建、查看 secret 与参数校验。
//
// 这些用例连接真实 PostgreSQL（LINA_TEST_DATABASE_DSN，未设置时用本地开发库），
// 每个用例使用随机 app_key 前缀并在 t.Cleanup 中硬删除自己写入的行，互不依赖执行顺序。

package apikey

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/plugin/capability/hostconfigcap"

	_ "lina-core/pkg/dbdriver"

	"lina-plugin-linapro-lark-push/backend/internal/dao"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
	"lina-plugin-linapro-lark-push/backend/internal/model/do"
)

// defaultTestDSN 是本地开发库连接串（与 hack/config.yaml 一致）。
const defaultTestDSN = "pgsql:postgres:postgres@tcp(127.0.0.1:5432)/linapro?sslmode=disable"

// fakeHostConfig 只回答 push_groups 配置键。
type fakeHostConfig struct {
	hostconfigcap.Service
	groups map[string]any
}

// Get 返回 push_groups 配置。
func (f *fakeHostConfig) Get(_ context.Context, key string, _ any) (*gvar.Var, error) {
	if key == larkcfg.ConfigKey && f.groups != nil {
		return gvar.New(f.groups), nil
	}
	return nil, nil
}

// testGroups 是用例使用的推送分组配置：ops-alert 与 linapro-eval-360 各一个推送应用，
// multi-app 有两个推送应用（用于「配置群聊须指定推送应用」）。
func testGroups() map[string]any {
	app := map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "通知", "identity_app_id": "cli_a"}
	appB := map[string]any{"app_id": "cli_push_b", "secret_key": "pb", "name": "通知B", "identity_app_id": "cli_b"}
	return map[string]any{"ops-alert": []any{app}, "linapro-eval-360": []any{app}, "multi-app": []any{app, appB}}
}

// setupDB 把包测试指向真实数据库、按安装 SQL 建表，并返回服务与本用例专用的 app_key 前缀。
func setupDB(t *testing.T) (context.Context, Service, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("LINA_TEST_DATABASE_DSN"))
	if dsn == "" {
		dsn = defaultTestDSN
	}
	original := gdb.GetAllConfig()
	if err := gdb.SetConfig(gdb.Config{gdb.DefaultGroupName: gdb.ConfigGroup{{Link: dsn}}}); err != nil {
		t.Fatalf("配置测试数据库失败: %v", err)
	}
	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, installSQL(t)); err != nil {
		t.Skipf("测试数据库不可用，跳过（%v）", err)
	}

	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("生成前缀: %v", err)
	}
	prefix := "ut-" + hex.EncodeToString(buf) + "-"
	t.Cleanup(func() {
		cols := dao.ApiKey.Columns()
		if _, err := dao.ApiKey.Ctx(ctx).Unscoped().WhereLike(cols.AppKey, prefix+"%").Delete(); err != nil {
			t.Errorf("清理测试数据失败: %v", err)
		}
		if err := g.DB().Close(ctx); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
		if err := gdb.SetConfig(original); err != nil {
			t.Errorf("还原数据库配置失败: %v", err)
		}
	})
	return ctx, New(&fakeHostConfig{groups: testGroups()}), prefix
}

// installSQL 读取插件安装 SQL（与生产建表同源）。
func installSQL(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "manifest", "sql", "001-linapro-lark-push-api-key.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取安装 SQL: %v", err)
	}
	return string(b)
}

// testSecret 是新增用例统一使用的合法 secret。
const testSecret = "unit-test-secret-0123456789"

// saveInput 构造一份合法的编辑输入（secret 留空，表示保持原值）。
func saveInput(appKey string) SaveInput {
	return SaveInput{Name: "运维告警", AppKey: appKey, Group: "ops-alert", Status: StatusNormal, Remark: "单测"}
}

// createInput 构造一份合法的新增输入：在 saveInput 基础上填入必填的 secret。
func createInput(appKey string) SaveInput {
	in := saveInput(appKey)
	in.Secret = testSecret
	return in
}

// TestCreate_RequiresSecret 验证新增时 secret 必填（FB-13）：留空或仅含空白都返回参数错误，
// 且在访问数据库前拒绝（本用例不连库）。
func TestCreate_RequiresSecret(t *testing.T) {
	svc := New(&fakeHostConfig{groups: testGroups()})
	for _, secret := range []string{"", "   "} {
		in := saveInput("ok-key")
		in.Secret = secret
		if _, err := svc.Create(context.Background(), in); !bizerr.Is(err, CodeApiKeyInvalidParam) {
			t.Fatalf("secret=%q: 新增应返回 CodeApiKeyInvalidParam, got %v", secret, err)
		}
	}
}

// TestCreate_SavesSecretAndHidesItInList 验证新增按提交的 secret 原样保存，
// 列表与详情不含 secret，GetSecret 可反复读到同一值。
func TestCreate_SavesSecretAndHidesItInList(t *testing.T) {
	ctx, svc, prefix := setupDB(t)
	out, err := svc.Create(ctx, createInput(prefix+"gen"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Secret != testSecret {
		t.Fatalf("应保存提交的 secret, got %q", out.Secret)
	}
	for i := 0; i < 2; i++ {
		secret, err := svc.GetSecret(ctx, out.Id)
		if err != nil || secret != out.Secret {
			t.Fatalf("第 %d 次查看 secret 不符: %q %v", i+1, secret, err)
		}
	}
	list, err := svc.List(ctx, ListInput{Keyword: prefix})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 1 || list.List[0].AppKey != prefix+"gen" || list.List[0].Group != "ops-alert" {
		t.Fatalf("列表结果不符: %+v", list)
	}
	if list.List[0].CreatedAt == nil || *list.List[0].CreatedAt <= 0 {
		t.Fatalf("创建时间应为毫秒时间戳: %+v", list.List[0])
	}
}

// TestCreate_AppKeyUniqueAndGroupChecked 验证 app_key 在未删除凭据中唯一、分组必须已配置。
func TestCreate_AppKeyUniqueAndGroupChecked(t *testing.T) {
	ctx, svc, prefix := setupDB(t)
	if _, err := svc.Create(ctx, createInput(prefix+"dup")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, createInput(prefix+"dup")); !bizerr.Is(err, CodeApiKeyAppKeyExists) {
		t.Fatalf("重复 app_key 应返回 CodeApiKeyAppKeyExists, got %v", err)
	}
	in := createInput(prefix + "grp")
	in.Group = "not-configured"
	if _, err := svc.Create(ctx, in); !bizerr.Is(err, CodeApiKeyGroupNotFound) {
		t.Fatalf("未配置分组应返回 CodeApiKeyGroupNotFound, got %v", err)
	}
}

// TestUpdate_KeepsSecretWhenEmpty 验证编辑时 secret 留空保持原值，填写则替换；app_key 不能改成别人的。
func TestUpdate_KeepsSecretWhenEmpty(t *testing.T) {
	ctx, svc, prefix := setupDB(t)
	in := saveInput(prefix + "upd")
	in.Secret = "manual-secret-0123456789"
	out, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	other, err := svc.Create(ctx, createInput(prefix+"other"))
	if err != nil {
		t.Fatalf("Create other: %v", err)
	}

	edit := saveInput(prefix + "upd")
	edit.Name = "改名"
	edit.Group = "linapro-eval-360"
	if err = svc.Update(ctx, out.Id, edit); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if secret, _ := svc.GetSecret(ctx, out.Id); secret != in.Secret {
		t.Fatalf("secret 留空应保持原值, got %q", secret)
	}
	item, err := svc.Get(ctx, out.Id)
	if err != nil || item.Name != "改名" || item.Group != "linapro-eval-360" {
		t.Fatalf("编辑结果不符: %+v %v", item, err)
	}

	edit.Secret = "replaced-secret-0123456789"
	if err = svc.Update(ctx, out.Id, edit); err != nil {
		t.Fatalf("Update secret: %v", err)
	}
	if secret, _ := svc.GetSecret(ctx, out.Id); secret != edit.Secret {
		t.Fatalf("填写 secret 应替换, got %q", secret)
	}

	clash := saveInput(prefix + "other")
	if err = svc.Update(ctx, out.Id, clash); !bizerr.Is(err, CodeApiKeyAppKeyExists) {
		t.Fatalf("改成他人的 app_key 应拒绝, got %v", err)
	}
	if err = svc.Update(ctx, other.Id, saveInput(prefix+"other")); err != nil {
		t.Fatalf("保持自身 app_key 不应判重复: %v", err)
	}
}

// TestSetStatusAndDelete_AffectLookup 验证停用后鉴权查不到、启用后恢复；软删除后查不到且同名可重建。
func TestSetStatusAndDelete_AffectLookup(t *testing.T) {
	ctx, svc, prefix := setupDB(t)
	appKey := prefix + "life"
	out, err := svc.Create(ctx, createInput(appKey))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cred, err := svc.FindEnabledByAppKey(ctx, appKey); err != nil || cred == nil || cred.Group != "ops-alert" || cred.Secret != out.Secret {
		t.Fatalf("启用凭据应可查到: %+v %v", cred, err)
	}

	if err = svc.SetStatus(ctx, out.Id, StatusDisabled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if cred, _ := svc.FindEnabledByAppKey(ctx, appKey); cred != nil {
		t.Fatalf("停用后不应查到: %+v", cred)
	}
	disabled := StatusDisabled
	if list, _ := svc.List(ctx, ListInput{Keyword: appKey, Status: &disabled}); list == nil || list.Total != 1 {
		t.Fatalf("按停用状态过滤应命中 1 条: %+v", list)
	}
	if err = svc.SetStatus(ctx, out.Id, StatusNormal); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	if err = svc.Delete(ctx, out.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cred, _ := svc.FindEnabledByAppKey(ctx, appKey); cred != nil {
		t.Fatalf("删除后不应查到: %+v", cred)
	}
	if _, err = svc.Get(ctx, out.Id); !bizerr.Is(err, CodeApiKeyNotFound) {
		t.Fatalf("删除后详情应 NotFound, got %v", err)
	}
	if n, _ := dao.ApiKey.Ctx(ctx).Unscoped().Where(do.ApiKey{Id: out.Id}).Count(); n != 1 {
		t.Fatalf("应为软删除（物理行仍在）, count=%d", n)
	}
	if _, err = svc.Create(ctx, createInput(appKey)); err != nil {
		t.Fatalf("删除后同名 app_key 应可重建: %v", err)
	}
}

// TestNormalizeSaveInput_Rejects 验证字段校验（纯函数，不连库）。
func TestNormalizeSaveInput_Rejects(t *testing.T) {
	cases := map[string]func(in *SaveInput){
		"名称为空":             func(in *SaveInput) { in.Name = " " },
		"app_key 含空格":      func(in *SaveInput) { in.AppKey = "a b" },
		"app_key 超长":       func(in *SaveInput) { in.AppKey = strings.Repeat("a", 65) },
		"分组为空":             func(in *SaveInput) { in.Group = "" },
		"secret 过短":        func(in *SaveInput) { in.Secret = "short" },
		"secret 含中文":       func(in *SaveInput) { in.Secret = "秘钥秘钥秘钥秘钥秘钥秘钥" },
		"状态非法":             func(in *SaveInput) { in.Status = 2 },
		"备注超长":             func(in *SaveInput) { in.Remark = strings.Repeat("备", 501) },
		"chat_id 非 oc_ 开头": func(in *SaveInput) { in.ChatID = "chat_123" },
		"chat_id 含非法字符":    func(in *SaveInput) { in.ChatID = "oc_12 34" },
		"无群聊却指定推送应用":       func(in *SaveInput) { in.ChatPushAppID = "cli_push_a" },
	}
	for name, mutate := range cases {
		in := saveInput("ok-key")
		mutate(&in)
		if _, err := normalizeSaveInput(in); !bizerr.Is(err, CodeApiKeyInvalidParam) {
			t.Fatalf("%s: want CodeApiKeyInvalidParam, got %v", name, err)
		}
	}
	in := saveInput(" ok.key-1_A ")
	in.Secret = " valid-secret-0123456789 "
	got, err := normalizeSaveInput(in)
	if err != nil || got.AppKey != "ok.key-1_A" || got.Secret != "valid-secret-0123456789" {
		t.Fatalf("合法输入应去首尾空白后通过: %+v %v", got, err)
	}
}

// TestCreate_ChatConfigPersistsAndReachesLookup 验证群聊配置随凭据保存：详情返回 chat_id 与推送应用，
// 签名鉴权查询（FindEnabledByAppKey）同样带出，编辑可清空。
func TestCreate_ChatConfigPersistsAndReachesLookup(t *testing.T) {
	ctx, svc, prefix := setupDB(t)
	in := createInput(prefix + "chat")
	in.ChatID = "oc_5ad11d72b830411d72b836c20"
	in.ChatPushAppID = "cli_push_a"
	out, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err := svc.Get(ctx, out.Id)
	if err != nil || item.ChatID != in.ChatID || item.ChatPushAppID != "cli_push_a" {
		t.Fatalf("详情应带群聊配置: %+v %v", item, err)
	}
	cred, err := svc.FindEnabledByAppKey(ctx, in.AppKey)
	if err != nil || cred == nil || cred.ChatID != in.ChatID || cred.ChatPushAppID != "cli_push_a" {
		t.Fatalf("鉴权查询应带群聊配置: %+v %v", cred, err)
	}

	in.ChatID, in.ChatPushAppID = "", ""
	if err = svc.Update(ctx, out.Id, in); err != nil {
		t.Fatalf("Update 清空群聊: %v", err)
	}
	if cred, err = svc.FindEnabledByAppKey(ctx, in.AppKey); err != nil || cred.ChatID != "" || cred.ChatPushAppID != "" {
		t.Fatalf("清空后鉴权查询不应再带群聊: %+v %v", cred, err)
	}
}

// TestCreate_ChatPushAppValidatedAgainstGroup 验证群聊推送应用按分组校验：单应用分组可省略，
// 多应用分组必须指定，指定的应用必须属于该分组；不合法时不落库。
func TestCreate_ChatPushAppValidatedAgainstGroup(t *testing.T) {
	ctx, svc, prefix := setupDB(t)

	single := createInput(prefix + "single")
	single.ChatID = "oc_single"
	if _, err := svc.Create(ctx, single); err != nil {
		t.Fatalf("单应用分组省略推送应用应成功: %v", err)
	}

	multi := createInput(prefix + "multi")
	multi.Group = "multi-app"
	multi.ChatID = "oc_multi"
	if _, err := svc.Create(ctx, multi); !bizerr.Is(err, CodeApiKeyInvalidParam) {
		t.Fatalf("多应用分组未指定推送应用应参数错误, got %v", err)
	}
	multi.ChatPushAppID = "cli_not_in_group"
	if _, err := svc.Create(ctx, multi); !bizerr.Is(err, CodeApiKeyInvalidParam) {
		t.Fatalf("推送应用不属于分组应参数错误, got %v", err)
	}
	multi.ChatPushAppID = "cli_push_b"
	if _, err := svc.Create(ctx, multi); err != nil {
		t.Fatalf("指定分组内推送应用应成功: %v", err)
	}
	if n, err := dao.ApiKey.Ctx(ctx).Where(do.ApiKey{AppKey: multi.AppKey}).Count(); err != nil || n != 1 {
		t.Fatalf("校验失败的保存不应落库，应只有 1 条: n=%d %v", n, err)
	}
}
