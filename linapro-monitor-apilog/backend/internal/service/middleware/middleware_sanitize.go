// middleware_sanitize.go 实现落库前的脱敏。脱敏必须在写库前完成：
// 未脱敏的敏感值不得进入数据库，也不得进入服务日志。

package middleware

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// 脱敏占位与字段名常量。
const (
	// redactedValue 是替换敏感值的占位。
	redactedValue = "[REDACTED]"
)

// redactedHeaderNames 是整体替换为占位的请求头（不区分大小写）。
//
// X-Emp-Token 与 X-Signature 当前分别属于考勤验证插件与消息推送插件（本期都不记录），
// 留在这里是为了将来接入时不必回头补规则。
var redactedHeaderNames = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"set-cookie":    {},
	"x-emp-token":   {},
	"x-signature":   {},
}

// defaultMaskedFields 是键名命中即整体脱敏的 JSON / 表单字段（不区分大小写）。
//
// 刻意不含 `code`：飞书预授权码虽敏感，但宿主统一响应结构里的 `code` 是整数业务错误码，
// 加进来会把每一条响应的错误码都变成占位，日志直接失去价值（见 design D5）。
var defaultMaskedFields = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"secret":        {},
	"secret_key":    {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"signature":     {},
	"authorization": {},
}

// maskFields 读取追加脱敏字段配置，与内置名单合并成一张查找表。
func (s *serviceImpl) maskFields(ctx context.Context) map[string]struct{} {
	fields := make(map[string]struct{}, len(defaultMaskedFields))
	for name := range defaultMaskedFields {
		fields[name] = struct{}{}
	}
	for _, name := range s.stringListConfig(ctx, configKeyMaskFields) {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized != "" {
			fields[normalized] = struct{}{}
		}
	}
	return fields
}

// isMaskedFieldName 判定字段名是否命中脱敏名单。
func isMaskedFieldName(key string, maskFields map[string]struct{}) bool {
	_, ok := maskFields[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// buildRecordHeaders 把请求头序列化为 JSON，敏感头整体替换为占位。
func buildRecordHeaders(request *ghttp.Request, maskFields map[string]struct{}) string {
	headers := make(map[string]string, len(request.Header))
	for name, values := range request.Header {
		if _, redact := redactedHeaderNames[strings.ToLower(name)]; redact {
			headers[name] = redactedValue
			continue
		}
		headers[name] = strings.Join(values, ", ")
	}
	if len(headers) == 0 {
		return ""
	}
	buffer, err := json.Marshal(headers)
	if err != nil {
		return ""
	}
	return string(buffer)
}

// buildRecordQuery 记录原始查询串，参数值按脱敏名单处理。
func buildRecordQuery(request *ghttp.Request, maskFields map[string]struct{}) string {
	if request.URL == nil || strings.TrimSpace(request.URL.RawQuery) == "" {
		return ""
	}
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		// 查询串无法解析时原样保留：脱敏失败不能连排查要素一起丢掉。
		return request.URL.RawQuery
	}
	redacted := false
	for name := range values {
		if isMaskedFieldName(name, maskFields) {
			values[name] = []string{redactedValue}
			redacted = true
		}
	}
	if !redacted {
		return request.URL.RawQuery
	}
	return values.Encode()
}

// sanitizeJSONText 递归脱敏一段 JSON 文本。非 JSON 文本原样返回（无法结构化脱敏）。
func sanitizeJSONText(text string, maskFields map[string]struct{}) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	// 只有对象/数组开头的文本才值得尝试解析，避免对大段纯文本做无谓的 unmarshal。
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return text
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return text
	}
	sanitized, changed := sanitizeJSONValue(decoded, maskFields)
	if !changed {
		return text
	}
	buffer, err := json.Marshal(sanitized)
	if err != nil {
		return text
	}
	return string(buffer)
}

// sanitizeJSONValue 遍历一个已解码的 JSON 值并替换命中名单的字段值。
func sanitizeJSONValue(value any, maskFields map[string]struct{}) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		changed := false
		for key, item := range typed {
			if isMaskedFieldName(key, maskFields) {
				sanitized[key] = redactedValue
				changed = true
				continue
			}
			child, childChanged := sanitizeJSONValue(item, maskFields)
			sanitized[key] = child
			changed = changed || childChanged
		}
		if !changed {
			return value, false
		}
		return sanitized, true

	case []any:
		sanitized := make([]any, len(typed))
		changed := false
		for index, item := range typed {
			child, childChanged := sanitizeJSONValue(item, maskFields)
			sanitized[index] = child
			changed = changed || childChanged
		}
		if !changed {
			return value, false
		}
		return sanitized, true
	}
	return value, false
}

// sanitizeFormValues 按同一名单脱敏表单字段。
func sanitizeFormValues(values map[string][]string, maskFields map[string]struct{}) map[string][]string {
	if len(values) == 0 {
		return values
	}
	sanitized := make(map[string][]string, len(values))
	for name, items := range values {
		if isMaskedFieldName(name, maskFields) {
			sanitized[name] = []string{redactedValue}
			continue
		}
		sanitized[name] = items
	}
	return sanitized
}
