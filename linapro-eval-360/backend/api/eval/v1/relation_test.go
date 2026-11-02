// relation_test.go 覆盖「新增评价关系」请求 DTO 的校验标签（FB-32）。
//
// 回归点：items 曾带 `max-length:500` 想限制条数，但 GoFrame 的 max-length 对切片按 JSON 文本长度计算，
// 几条评价关系的 JSON 就超过 500 字符，正常批量会被误拒。条数上限现只在服务层校验；
// 本测试同时确认 GoFrame 会递归校验 items 内每个条目的标签（姓名必填、职务长度上限）。

package v1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
)

// validItems 构造 n 条合法的待新增评价关系。
func validItems(n int) []*AddRelationItem {
	items := make([]*AddRelationItem, 0, n)
	for i := range n {
		items = append(items, &AddRelationItem{
			EvaluateeName:       fmt.Sprintf("被评估人%d", i),
			EvaluateeJobTitle:   "销售总监",
			EvaluateeDepartment: "华东销售部",
			EvaluatorName:       fmt.Sprintf("评价者%d", i),
			RoleName:            "上级",
		})
	}
	return items
}

// TestAddRelationReqValidation 覆盖批量条目放行、空条目拒绝与逐条目字段校验。
func TestAddRelationReqValidation(t *testing.T) {
	ctx := context.Background()
	validate := func(req *AddRelationReq) error {
		return g.Validator().Data(req).Run(ctx)
	}

	// 50 条合法条目（JSON 远超 500 字符）必须放行。
	if err := validate(&AddRelationReq{QuestionnaireId: 1, Items: validItems(50)}); err != nil {
		t.Fatalf("50 条合法条目不应被拦截：%v", err)
	}

	if err := validate(&AddRelationReq{QuestionnaireId: 1}); err == nil {
		t.Fatal("items 为空应被拦截")
	}

	missingName := validItems(3)
	missingName[2].EvaluatorName = ""
	if err := validate(&AddRelationReq{QuestionnaireId: 1, Items: missingName}); err == nil {
		t.Fatal("条目缺少评价者姓名应被拦截")
	}

	longTitle := validItems(1)
	longTitle[0].EvaluateeJobTitle = strings.Repeat("职", 101)
	if err := validate(&AddRelationReq{QuestionnaireId: 1, Items: longTitle}); err == nil {
		t.Fatal("职务超过 100 字应被拦截")
	}
}
