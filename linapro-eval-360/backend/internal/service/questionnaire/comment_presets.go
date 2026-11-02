// 本文件提供矩阵评价项「预设填空候选」的序列化与反序列化纯工具函数（可独立单测）。
//
// 候选在库中以 JSON 数组文本存放（如 ["沟通清晰","逻辑严谨"]），空串表示无预设。
// 选用 JSON 而非逗号分隔，是因为候选是用户自由录入的中文文案，可能含逗号、顿号等分隔字符。
package questionnaire

import (
	"encoding/json"
	"strings"
)

// encodeCommentPresets 把候选文案切片序列化为 JSON 数组文本。
// 逐项去首尾空白、丢弃空项并保序去重；结果为空时返回空串（语义：无预设，员工端自由输入）。
func encodeCommentPresets(presets []string) (string, error) {
	cleaned := make([]string, 0, len(presets))
	seen := make(map[string]struct{}, len(presets))
	for _, p := range presets {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// decodeCommentPresets 把库中的 JSON 数组文本解析为候选文案切片。
// 空串或非法 JSON 一律返回空切片：历史行与脏数据按「无预设」降级，不阻断取题。
func decodeCommentPresets(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	out := make([]string, 0, 4)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}
