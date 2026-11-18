// middleware_payload.go 实现请求体与响应体的捕获。
//
// 判定与读取都发生在 Next() 返回之后：
//   - 非 multipart / 非二进制：GetBodyString()。命中 GoFrame 的 bodyContent 缓存时零成本；
//     缓存为空时读原始 Body——「处理器压根没读」正是最需要看到提交内容的时刻（例如鉴权在解析前就拒绝）。
//   - multipart：读已解析的 MultipartForm，文件只记元数据，不缓冲内容。
//   - 二进制：只计数。
//
// GetBodyString 在读失败时是 panic（MaxBytesReader 已触发上限、或下游关闭了 Body），
// 这里必须 recover：记录日志的代价绝不能是把业务响应变成 500。

package middleware

import (
	"fmt"
	"mime"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// 请求体与响应体的占位文案。
const (
	// placeholderBodyUnreadable 表示 Body 已经读不出来（下游关闭，或路由请求体上限已触发）。
	placeholderBodyUnreadable = "[BODY 不可读]"
	// placeholderBodyNotRetained 表示 Body 被下游读走且没有归还，记录不到内容。
	placeholderBodyNotRetained = "[BODY 未留存 content-length=%d]"
	// placeholderMultipartUnparsed 表示 multipart 未被解析。
	placeholderMultipartUnparsed = "[multipart 未解析] content-length=%d"
	// placeholderStream 表示响应已 hijack 或被流式输出。
	placeholderStream = "[STREAM]"
)

// buildRecordRequestBody 捕获请求体并脱敏。
func buildRecordRequestBody(request *ghttp.Request, maskFields map[string]struct{}) string {
	contentType := request.GetHeader("Content-Type")
	switch {
	case isBinaryContentType(contentType):
		// 二进制请求体只计数，不缓冲。
		return buildBinaryPlaceholder(contentType, request.ContentLength)
	case isMultipartContentType(contentType):
		return buildMultipartBody(request, maskFields)
	}
	return buildTextRequestBody(request, maskFields)
}

// buildTextRequestBody 捕获非 multipart 的文本请求体。
func buildTextRequestBody(request *ghttp.Request, maskFields map[string]struct{}) string {
	body := readBodySafely(request)
	if body == "" && request.ContentLength > 0 {
		// 声明了长度却读不到内容：下游读走了且没有归还。这是将来新增插件需要留意的情形，
		// 显式写出来，比留一个空串更容易排查。
		return fmt.Sprintf(placeholderBodyNotRetained, request.ContentLength)
	}
	return sanitizeJSONText(body, maskFields)
}

// readBodySafely 读取请求体，把「读不出来」收敛成占位文案而不是 panic 或错误。
func readBodySafely(request *ghttp.Request) (body string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// MaxBytesReader 会把错误 latch 住（任何后续读取都复现同一个错误）；
			// 另一种可能是下游关闭了 Body。两种都不该让整条记录丢失。
			body = placeholderBodyUnreadable
		}
	}()
	return request.GetBodyString()
}

// buildMultipartBody 记录 multipart 的普通字段与文件元数据。
//
// multipart 的 bodyContent 缓存恒为空（GoFrame 的 parseForm 只对非 multipart 做可重复读取），
// 因此只能读已解析的 MultipartForm。
func buildMultipartBody(request *ghttp.Request, maskFields map[string]struct{}) string {
	form := request.Request.MultipartForm
	if form == nil {
		// 处理器在解析前就拒绝（例如鉴权失败）：如实说明没解析，而不是记成空。
		return fmt.Sprintf(placeholderMultipartUnparsed, request.ContentLength)
	}

	var builder strings.Builder
	fields := sanitizeFormValues(form.Value, maskFields)
	for name, values := range fields {
		builder.WriteString(name)
		builder.WriteString("=")
		builder.WriteString(strings.Join(values, ","))
		builder.WriteString("; ")
	}
	for name, files := range form.File {
		for _, file := range files {
			fmt.Fprintf(&builder, "[FILE %s name=%s type=%s size=%d]; ",
				name, file.Filename, file.Header.Get("Content-Type"), file.Size)
		}
	}
	return strings.TrimSpace(builder.String())
}

// buildRecordResponseBody 捕获响应体并脱敏。
func buildRecordResponseBody(request *ghttp.Request, scope recordScope, maskFields map[string]struct{}) string {
	var (
		contentType = request.Response.Header().Get("Content-Type")
		body        = request.Response.BufferString()
	)
	if contentType == "" {
		// 动态插件的桥接响应把内容类型写在元数据里。
		contentType = scope.responseContentType
	}
	if body == "" {
		// 动态插件的原始响应体由运行时分发器提前捕获在元数据里。
		body = scope.responseBody
	}
	return sanitizeResponsePayload(contentType, body, request.Response.IsHijacked(), maskFields)
}

// sanitizeResponsePayload 是响应体脱敏的纯函数部分：判定二进制 / 流式，其余按 JSON 脱敏。
//
// 抽出来是为了让单测不必构造完整的 ghttp.Response（其构造函数未导出）。
func sanitizeResponsePayload(
	contentType string,
	body string,
	hijacked bool,
	maskFields map[string]struct{},
) string {
	if hijacked {
		// 已接管连接（SSE、长轮询等）的响应没有可读缓冲。
		return placeholderStream
	}
	if isBinaryContentType(contentType) {
		return buildBinaryPlaceholder(contentType, int64(len(body)))
	}
	return sanitizeJSONText(body, maskFields)
}

// buildBinaryPlaceholder 构造二进制内容的占位文案。
func buildBinaryPlaceholder(contentType string, size int64) string {
	if size < 0 {
		size = 0
	}
	return fmt.Sprintf("[BINARY %s %d bytes]", contentType, size)
}

// isMultipartContentType 判定是否为 multipart 请求。
func isMultipartContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "multipart/")
	}
	return strings.HasPrefix(mediaType, "multipart/")
}

// isBinaryContentType 判定内容类型是否为二进制。
//
// multipart 在调用处已先行分流，因此这里只负责「整体不可按文本读」的类型。
func isBinaryContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	lower := strings.ToLower(contentType)
	return strings.Contains(lower, "application/octet-stream") ||
		strings.Contains(lower, "spreadsheetml") ||
		strings.Contains(lower, "application/pdf") ||
		strings.Contains(lower, "application/zip") ||
		strings.HasPrefix(lower, "image/") ||
		strings.HasPrefix(lower, "audio/") ||
		strings.HasPrefix(lower, "video/")
}
