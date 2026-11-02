// 本文件提供二维题目可见性的纯工具函数（可独立单测，见 tasks 11.1）。
package questionnaire

import (
	"strconv"
	"strings"
)

// IsQuestionVisible 判断某评价者以某角色作答时能否看到该题（二维 AND 语义）。
//
// 条件1（visibleRoleIDs）：为空 = 所有能看到该问卷的人可见；非空 = 评价者角色须在其中。
// 条件2（excludedEvaluatorEmpIDs）：受限评价者排除名单，按评价者本人 EmpID 匹配；
// 评价者本人在名单内 = 不可见。该名单对评价者全局生效，与其所评的被评估人无关。
//
// 二者以 AND 取交集：可见 当且仅当 满足条件1 且 评价者本人不在条件2 名单内。
func IsQuestionVisible(evaluatorRoleID, evaluatorEmpID int64, visibleRoleIDs, excludedEvaluatorEmpIDs []int64) bool {
	if len(visibleRoleIDs) > 0 && !containsInt64(visibleRoleIDs, evaluatorRoleID) {
		return false
	}
	if containsInt64(excludedEvaluatorEmpIDs, evaluatorEmpID) {
		return false
	}
	return true
}

// containsInt64 判断切片是否含某值。
func containsInt64(list []int64, target int64) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

// parseCSVInt64 把逗号分隔的 id 字符串解析为去空、去零、去重后的 int64 切片。
// 空字符串返回空切片（语义：全员可见 / 无排除）。
func parseCSVInt64(csv string) []int64 {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return []int64{}
	}
	var (
		out  = make([]int64, 0, 8)
		seen = make(map[int64]struct{}, 8)
	)
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// joinCSVInt64 把 int64 切片拼为逗号分隔字符串（去零、去重、保持顺序）。
func joinCSVInt64(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	var (
		parts = make([]string, 0, len(ids))
		seen  = make(map[int64]struct{}, len(ids))
	)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}
