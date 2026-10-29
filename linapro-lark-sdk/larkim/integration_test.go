//go:build integration

// 飞书真机集成测试：真发一条文本到指定用户。
//
// 运行方式（凭据不落库、不进代码，只经 -ldflags 之外的 flag 传入）：
//
//	go test -tags=integration ./larkim/ -run TestSendTextIntegration -v \
//	  -app-id cli_xxx -app-secret xxx -user-id on_xxx
//
// 未提供 flag 时用例自行 Skip，不会在 CI 里误发消息。
package larkim

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

var (
	flagAppID     = flag.String("app-id", "", "飞书推送应用 App ID（integration 用）")
	flagAppSecret = flag.String("app-secret", "", "飞书推送应用 App Secret（integration 用）")
	flagUserID    = flag.String("user-id", "", "已知的飞书 user_id（integration 用）")
)

// TestSendTextIntegration 真发一条文本，验证「凭据 + 可见范围 + user_id 权限」三件套。
func TestSendTextIntegration(t *testing.T) {
	if *flagAppID == "" || *flagAppSecret == "" || *flagUserID == "" {
		t.Skip("未提供 -app-id/-app-secret/-user-id，跳过真机测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient(*flagAppID, *flagAppSecret)
	text := "linapro-lark-sdk 集成测试：" + time.Now().Format(time.RFC3339)
	msgID, err := client.SendText(ctx, ReceiveIDTypeUserID, *flagUserID, text)
	if err != nil {
		t.Fatalf("SendText 失败（检查应用可见范围与 im:message:send_as_bot 权限）: %v", err)
	}
	if msgID == "" {
		t.Fatal("应返回非空 message_id")
	}
	t.Logf("已发送 message_id=%s", msgID)
}

// TestSendTextIntegrationErrorMapping 用明显错误的 user_id 验证错误映射带出 code 与 req_id。
func TestSendTextIntegrationErrorMapping(t *testing.T) {
	if *flagAppID == "" || *flagAppSecret == "" {
		t.Skip("未提供 -app-id/-app-secret，跳过真机测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient(*flagAppID, *flagAppSecret)
	_, err := client.SendText(ctx, ReceiveIDTypeUserID, "on_this_user_does_not_exist", "integration error mapping")
	if err == nil {
		t.Fatal("错误 user_id 应返回错误")
	}
	var fe *FeishuError
	if !errors.As(err, &fe) {
		t.Fatalf("错误应可解析为 *FeishuError，实际 %T: %v", err, err)
	}
	if fe.Code == 0 || fe.ReqID == "" {
		t.Fatalf("FeishuError 应带 code 与 req_id: %+v", fe)
	}
	if !strings.Contains(fe.Error(), "req_id=") {
		t.Fatalf("错误文本应包含 req_id: %s", fe.Error())
	}
}
