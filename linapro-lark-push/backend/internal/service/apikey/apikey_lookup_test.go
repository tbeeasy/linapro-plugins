// apikey_lookup_test.go 覆盖 apikey_lookup.go：分组下拉读取 push_groups，配置缺失时报错；
// 空 app_key 不查库。

package apikey

import (
	"context"
	"reflect"
	"testing"

	"lina-core/pkg/bizerr"
)

// TestListGroups 验证分组按名升序返回且带各自的推送应用（不含密钥），宿主配置缺失时返回
// CodeApiKeyGroupsUnavailable。
func TestListGroups(t *testing.T) {
	svc := New(&fakeHostConfig{groups: testGroups()})
	got, err := svc.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	appA := PushAppOption{AppID: "cli_push_a", Name: "通知"}
	want := []GroupOption{
		{Name: "linapro-eval-360", PushApps: []PushAppOption{appA}},
		{Name: "multi-app", PushApps: []PushAppOption{appA, {AppID: "cli_push_b", Name: "通知B"}}},
		{Name: "ops-alert", PushApps: []PushAppOption{appA}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}

	if _, err = New(nil).ListGroups(context.Background()); !bizerr.Is(err, CodeApiKeyGroupsUnavailable) {
		t.Fatalf("宿主配置缺失应返回 CodeApiKeyGroupsUnavailable, got %v", err)
	}
	broken := New(&fakeHostConfig{groups: map[string]any{"ops-alert": "not-an-array"}})
	if _, err = broken.ListGroups(context.Background()); !bizerr.Is(err, CodeApiKeyGroupsUnavailable) {
		t.Fatalf("配置结构非法应返回 CodeApiKeyGroupsUnavailable, got %v", err)
	}
}

// TestFindEnabledByAppKey_Blank 验证空 app_key 直接返回未命中，不触达数据库。
func TestFindEnabledByAppKey_Blank(t *testing.T) {
	cred, err := New(nil).FindEnabledByAppKey(context.Background(), "  ")
	if err != nil || cred != nil {
		t.Fatalf("空 app_key 应返回 (nil, nil), got %+v %v", cred, err)
	}
}
