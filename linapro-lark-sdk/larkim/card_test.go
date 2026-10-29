package larkim

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// TestBuildTemplateCard 验证模板卡片 content 结构与飞书约定一致：
// {"type":"template","data":{"template_id","template_version_name","template_variable"}}。
func TestBuildTemplateCard(t *testing.T) {
	got, err := BuildTemplateCard("AAqyjwfhabcef", "1.0.0", map[string]any{
		"name":                   "张三",
		"dynamic_evaluation_url": "https://emp.example.com/eval-360/emp/todo?larkAppId=cli_a",
	})
	if err != nil {
		t.Fatalf("BuildTemplateCard: %v", err)
	}
	var raw struct {
		Type string `json:"type"`
		Data struct {
			TemplateID          string            `json:"template_id"`
			TemplateVersionName string            `json:"template_version_name"`
			TemplateVariable    map[string]string `json:"template_variable"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("模板卡片 JSON 非法: %v\n%s", err, got)
	}
	if raw.Type != "template" {
		t.Fatalf("type want template, got %q", raw.Type)
	}
	if raw.Data.TemplateID != "AAqyjwfhabcef" || raw.Data.TemplateVersionName != "1.0.0" {
		t.Fatalf("模板 ID/版本不符: %+v", raw.Data)
	}
	if raw.Data.TemplateVariable["name"] != "张三" {
		t.Fatalf("变量 name 不符: %v", raw.Data.TemplateVariable)
	}
	if raw.Data.TemplateVariable["dynamic_evaluation_url"] != "https://emp.example.com/eval-360/emp/todo?larkAppId=cli_a" {
		t.Fatalf("变量 dynamic_evaluation_url 不符: %v", raw.Data.TemplateVariable)
	}
}

// TestBuildTemplateCard_OmitEmptyVersion 验证版本名与变量为空时省略对应字段（由飞书取模板当前发布版本）。
func TestBuildTemplateCard_OmitEmptyVersion(t *testing.T) {
	got, err := BuildTemplateCard("tpl", "", nil)
	if err != nil {
		t.Fatalf("BuildTemplateCard: %v", err)
	}
	var raw struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("模板卡片 JSON 非法: %v", err)
	}
	if _, ok := raw.Data["template_version_name"]; ok {
		t.Fatalf("版本名为空时不应输出 template_version_name: %s", got)
	}
	if _, ok := raw.Data["template_variable"]; ok {
		t.Fatalf("变量为空时不应输出 template_variable: %s", got)
	}
}

// TestBuildTemplateCard_EmptyTemplateID 验证模板 ID 为空时拒绝构造。
func TestBuildTemplateCard_EmptyTemplateID(t *testing.T) {
	if _, err := BuildTemplateCard("", "1.0.0", nil); err == nil {
		t.Fatal("模板 ID 为空时应返回错误")
	}
}

// TestSendInteractiveCard_UnchangedBehavior 验证抽取 createMessage 后卡片路径行为不变：
// 仍以 msg_type=interactive 发送原样 content，错误信息仍称「交互卡片」。
func TestSendInteractiveCard_UnchangedBehavior(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		f := &fakeMessageCreator{resp: okResp("om_card")}
		card := `{"type":"template","data":{"template_id":"tpl"}}`
		got, err := newTestClient(f).SendInteractiveCard(context.Background(), ReceiveIDTypeOpenID, "ou_1", card)
		if err != nil {
			t.Fatalf("SendInteractiveCard: %v", err)
		}
		if got != "om_card" {
			t.Fatalf("message_id want om_card, got %q", got)
		}
		body := bodyOf(t, f)
		if b := larkcore.StringValue(body.MsgType); b != msgTypeInteractive {
			t.Fatalf("msg_type want %q, got %q", msgTypeInteractive, b)
		}
		if b := larkcore.StringValue(body.Content); b != card {
			t.Fatalf("content 应原样下发，got %s", b)
		}
	})

	t.Run("飞书失败信息", func(t *testing.T) {
		f := &fakeMessageCreator{resp: errResp(230002, "bad", "req-card")}
		_, err := newTestClient(f).SendInteractiveCard(context.Background(), ReceiveIDTypeOpenID, "ou_1", "{}")
		if err == nil {
			t.Fatal("飞书返回失败时应返回错误")
		}
		if msg := err.Error(); !strings.Contains(msg, "发送交互卡片失败") || !strings.Contains(msg, "req-card") {
			t.Fatalf("错误信息应保持卡片措辞并带 req_id: %s", msg)
		}
	})
}
