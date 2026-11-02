// comment_presets_test.go 覆盖矩阵评价项预设填空候选的序列化与反序列化（FB-26）。
package questionnaire

import (
	"reflect"
	"testing"
)

// TestEncodeDecodeCommentPresets 验证候选落库与读取的往返一致性：
// 空值按「无预设」处理；空白项丢弃、重复项去重；非法 JSON 降级为空切片而不报错。
func TestEncodeDecodeCommentPresets(t *testing.T) {
	cases := []struct {
		name   string
		input  []string
		want   string
		wantRT []string
	}{
		{name: "空候选存空串", input: nil, want: "", wantRT: []string{}},
		{name: "全空白等于空候选", input: []string{"  ", ""}, want: "", wantRT: []string{}},
		{
			name:   "去首尾空白并保序",
			input:  []string{" 沟通清晰 ", "逻辑严谨", "推动力强"},
			want:   `["沟通清晰","逻辑严谨","推动力强"]`,
			wantRT: []string{"沟通清晰", "逻辑严谨", "推动力强"},
		},
		{
			name:   "重复项去重",
			input:  []string{"沟通清晰", "沟通清晰", " 逻辑严谨 "},
			want:   `["沟通清晰","逻辑严谨"]`,
			wantRT: []string{"沟通清晰", "逻辑严谨"},
		},
		{
			// 候选里本身含逗号，JSON 承载不会与分隔符冲突（这是不用 CSV 的原因）。
			name:   "候选含逗号与引号",
			input:  []string{`沟通清晰，逻辑严谨`, `自评"优秀"`},
			want:   `["沟通清晰，逻辑严谨","自评\"优秀\""]`,
			wantRT: []string{"沟通清晰，逻辑严谨", `自评"优秀"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeCommentPresets(tc.input)
			if err != nil {
				t.Fatalf("序列化不应失败，err=%v", err)
			}
			if got != tc.want {
				t.Fatalf("期望 %q，实际 %q", tc.want, got)
			}
			if rt := decodeCommentPresets(got); !reflect.DeepEqual(rt, tc.wantRT) {
				t.Fatalf("反序列化期望 %v，实际 %v", tc.wantRT, rt)
			}
		})
	}
}

// TestDecodeCommentPresetsToleratesDirtyData 验证历史行/脏数据不阻断取题：空串与非法 JSON 均返回空切片。
func TestDecodeCommentPresetsToleratesDirtyData(t *testing.T) {
	for _, raw := range []string{"", "   ", "not-json", "[1,2", `{"a":1}`} {
		if got := decodeCommentPresets(raw); len(got) != 0 {
			t.Fatalf("%q 应降级为空切片，实际 %v", raw, got)
		}
	}
}
