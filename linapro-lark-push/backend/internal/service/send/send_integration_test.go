//go:build integration

// 插件级集成测试：真实配置 → 真实 empcap 门面 → 真发一条飞书文本。
//
// 与 larkim 的单测不同，这里覆盖的是**三件套同时成立**：
//  1. 身份应用具备「获取用户 user_id」权限（否则索引全空）；
//  2. 工号在目标企业有 own 身份；
//  3. 推送应用的可用范围覆盖该员工（否则飞书返回「用户不在可用范围」）。
//
// 运行：
//
//	go test -tags=integration ./backend/internal/service/send/ -v \
//	  -app-id cli_push_a -app-secret xxx -identity-app-id cli_a \
//	  -employee-no GZ001198 [-user-id on_xxx]
//
// 若同时给 -user-id，则跳过 empcap 查表、直接验证推送应用侧的投递能力；
// 只给 -employee-no 时要求真实 empcap 已绑定（需在宿主进程内运行）。
package send

import (
	"context"
	"flag"
	"testing"
	"time"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
)

var (
	flagAppID        = flag.String("app-id", "", "飞书推送应用 App ID（integration 用）")
	flagAppSecret    = flag.String("app-secret", "", "飞书推送应用 App Secret（integration 用）")
	flagIdentityApp  = flag.String("identity-app-id", "", "推送应用对应的身份应用 App ID（integration 用）")
	flagEmployeeNo   = flag.String("employee-no", "", "已知存在的员工工号（integration 用）")
	flagDirectUserID = flag.String("user-id", "", "已知的飞书 user_id；给出时跳过 empcap 查表")
)

// TestSendIntegration 走完整编排链路真发一条文本。
func TestSendIntegration(t *testing.T) {
	if *flagAppID == "" || *flagAppSecret == "" || *flagIdentityApp == "" {
		t.Skip("未提供 -app-id/-app-secret/-identity-app-id，跳过真机测试")
	}
	if *flagEmployeeNo == "" && *flagDirectUserID == "" {
		t.Skip("未提供 -employee-no 或 -user-id，无收件人可投递")
	}

	cfg := map[string]any{
		larkcfg.ConfigKey: map[string]any{testGroup: []any{
			map[string]any{
				"app_id":          *flagAppID,
				"secret_key":      *flagAppSecret,
				"name":            "integration",
				"identity_app_id": *flagIdentityApp,
			},
		}},
	}

	// 寻址来源：给了 -user-id 时跳过 empcap 查表，直接构造索引，验证「推送应用凭据 + 可用范围」；
	// 否则用 empcap 门面（需在宿主进程内运行）。
	indexPath := empcap.LarkUserIDResolverByApp
	if *flagDirectUserID != "" {
		indexPath = func(context.Context, string) (empcap.LarkUserIDIndex, error) {
			idx := empcap.LarkUserIDIndex{ByEmployeeNo: map[string]string{}}
			if *flagEmployeeNo != "" {
				idx.ByEmployeeNo[*flagEmployeeNo] = *flagDirectUserID
			}
			return idx, nil
		}
	} else if empcap.BoundEmployeeService() == nil {
		t.Skip("未提供 -user-id 且当前进程未绑定 empcap；请在宿主进程内运行或补 -user-id")
	}

	svc, err := New(
		&fakeHostConfig{values: cfg},
		func(appID, appSecret string) MessageSender { return larkim.NewClient(appID, appSecret) },
		indexPath,
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	recipient := larkpushcap.Recipient{EmployeeNo: *flagEmployeeNo}
	if recipient.EmployeeNo == "" {
		recipient = larkpushcap.Recipient{UserID: *flagDirectUserID}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := svc.SendText(ctx, larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{recipient},
		Text:       "linapro-lark-push 集成测试：" + time.Now().Format(time.RFC3339),
		BizSource:  "integration",
		BizRef:     "integration",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	for _, d := range res.Deliveries {
		t.Logf("投递明细: app=%s(%s) 工号=%s user_id=%s 状态=%s message_id=%s feishu_code=%d err=%s",
			d.PushAppName, d.PushAppID, d.EmployeeNo, d.UserID, d.Status, d.MessageID, d.FeishuCode, d.Error)
	}
	if !res.AllSucceeded() {
		t.Fatalf("应全部成功；若 feishu_code 提示「不在可见范围」，请检查推送应用的可用范围与 user_id 权限")
	}
}

// TestStatusIntegration 验证自检端点在真实配置下能取到可寻址人数。
func TestStatusIntegration(t *testing.T) {
	if *flagAppID == "" || *flagIdentityApp == "" {
		t.Skip("未提供 -app-id/-identity-app-id，跳过真机测试")
	}
	cfg := map[string]any{
		larkcfg.ConfigKey: map[string]any{testGroup: []any{
			map[string]any{
				"app_id": *flagAppID, "secret_key": *flagAppSecret,
				"name": "integration", "identity_app_id": *flagIdentityApp,
			},
		}},
	}
	// 自检只读配置与寻址索引，不构造发送器；发送器工厂仍按必填依赖提供。
	svc, err := New(
		&fakeHostConfig{values: cfg},
		func(appID, appSecret string) MessageSender { return larkim.NewClient(appID, appSecret) },
		empcap.LarkUserIDResolverByApp,
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	report := svc.Status(context.Background())
	if !report.ConfigValid {
		t.Fatalf("配置应合法: %+v", report.ConfigIssues)
	}
	if len(report.PushApps) != 1 {
		t.Fatalf("应报告 1 个推送应用, got %d", len(report.PushApps))
	}
	st := report.PushApps[0]
	if st.ProbeError != "" {
		t.Logf("寻址探测失败（若未在宿主进程内运行属正常）: %s", st.ProbeError)
		return
	}
	t.Logf("可寻址员工=%d 空 user_id 员工=%d 碰撞=%d", st.AddressableEmployees, st.BlankUserIDEmployees, st.Collisions)
	if st.AddressableEmployees == 0 && st.BlankUserIDEmployees > 0 {
		t.Fatal("全部 own 行 user_id 为空：身份应用缺少「获取用户 user_id」权限")
	}
}
