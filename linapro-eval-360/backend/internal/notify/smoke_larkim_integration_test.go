//go:build integration

// smoke_larkim_integration_test.go 是 tasks 8.3 的真机冒烟手段：用真实**推送应用**凭据
// （linapro-lark-push 中 push_groups.linapro-eval-360 的一项）向一个真实 user_id 发送一张 360 测评模板卡片
// （与生产同一模板变量 name / dynamic_evaluation_url、同一 user_id 寻址），验证推送应用
// 凭据、卡片模板授权与链接拼接可用。
//
// 本测试带 `integration` build tag，默认不随 `go test ./...` 运行，需显式开启：
//
//	go test -tags=integration -run TestLarkimSmokeSendCard -v \
//	  ./apps/lina-plugins/linapro-eval-360/backend/internal/notify/... \
//	  -args -lark_push_app_id=cli_xxx -lark_push_app_secret=xxx \
//	  -lark_identity_app_id=cli_yyy -lark_user_id=xxx \
//	  -lark_card_template_id=AAqxxx -lark_card_template_version=1.0.0 \
//	  -todo_h5_url='https://host/admin/#/eval-360/emp/todo'
//
// 前置（部署方/开发手动准备，本测试不自动满足）：
//   - 推送应用已开通 im:message:send_as_bot，可用范围覆盖接收者；
//   - identity_app_id 为同企业的身份（登录）应用，员工点开卡片后按它换票；
//   - user_id 为该企业下一名真实成员的 user_id（该成员会收到一张卡片）；
//   - 卡片模板已在飞书卡片搭建工具发布、授权给该推送应用，且声明变量 name 与 dynamic_evaluation_url。
//
// 未提供 flag 时 t.Skip 跳过（不视为失败），避免污染常规门禁。
package notify

import (
	"context"
	"flag"
	"testing"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"

	"linapro-lark-sdk/larkim"
)

var (
	smokePushAppID     = flag.String("lark_push_app_id", "", "真机冒烟用推送应用 app_id")
	smokePushAppSecret = flag.String("lark_push_app_secret", "", "真机冒烟用推送应用 app_secret")
	smokeIdentityAppID = flag.String("lark_identity_app_id", "", "同企业身份应用 app_id（写入链接 larkAppId）")
	smokeUserID        = flag.String("lark_user_id", "", "真机冒烟接收者 user_id（须属于该企业）")
	smokeTplID         = flag.String("lark_card_template_id", "", "真机冒烟用飞书卡片模板 ID")
	smokeTplVer        = flag.String("lark_card_template_version", "", "真机冒烟用飞书卡片模板版本名（可空）")
	smokeTodoURL       = flag.String("todo_h5_url", "", "员工端「我的待办」H5 完整 URL")
)

// TestLarkimSmokeSendCard 用推送应用凭据真发一张 360 测评模板卡片到指定 user_id，
// 断言返回非空 message_id（飞书 IM 接口成功投递的标志）。
//
// 卡片内容经生产同一 buildCard 构造（模板选取、身份应用 larkAppId、AppLink 包装），
// 发送与 linapro-lark-push 一致：推送应用凭据 + user_id。失败原因（权限缺失、可用范围
// 不含接收者、模板未授权给推送应用 / 变量不匹配、限流等）会由 larkim 返回带
// req_id/code/msg 的 gerror。
func TestLarkimSmokeSendCard(t *testing.T) {
	if *smokePushAppID == "" || *smokePushAppSecret == "" || *smokeIdentityAppID == "" ||
		*smokeUserID == "" || *smokeTplID == "" || *smokeTodoURL == "" {
		t.Skip("真机冒烟需 -lark_push_app_id/-lark_push_app_secret/-lark_identity_app_id/-lark_user_id/-lark_card_template_id/-todo_h5_url，未提供则跳过")
	}
	card := cardTemplate{
		todoURL: *smokeTodoURL,
		appTemplates: map[string]cardTemplateEntry{
			*smokePushAppID: {templateID: *smokeTplID, versionName: *smokeTplVer},
		},
	}
	app := larkpushcap.PushApp{AppID: *smokePushAppID, IdentityAppID: *smokeIdentityAppID}
	cardJSON, err := buildCard(card, app, "冒烟测试")
	if err != nil {
		t.Fatalf("构造卡片失败: %v", err)
	}
	client := larkim.NewClient(*smokePushAppID, *smokePushAppSecret)
	msgID, err := client.SendInteractiveCard(context.Background(), larkim.ReceiveIDTypeUserID, *smokeUserID, cardJSON)
	if err != nil {
		t.Fatalf("真机发送模板卡片失败（检查推送应用权限/可用范围/模板授权与变量）: %v", err)
	}
	if msgID == "" {
		t.Fatal("发送成功但未返回 message_id")
	}
	t.Logf("真机冒烟成功，message_id=%s", msgID)
}
