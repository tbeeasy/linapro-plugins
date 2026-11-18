// bindlog_body.go 把绑定失败的请求体裁剪为一行可读摘要。
//
// 只为排障：请求体可能很大（文件接口上限 31 MB），也可能含二进制。日志只保留能定位问题的部分——
// 普通请求体按字节上限截断，multipart 只记字段名与字段值、文件名与大小，绝不记文件内容。

package bindlog

import (
	"mime/multipart"
	"sort"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

const (
	// maxBodyLogBytes 是普通请求体进日志的字节上限：足够看清字段名与短取值，避免刷屏。
	maxBodyLogBytes = 2 << 10
	// maxFormValueLogBytes 是单个表单字段值进日志的字节上限。
	maxFormValueLogBytes = 256
)

// summarizeBody 返回请求体的单行摘要。multipart 走表单摘要（不记文件内容），其余按上限截断。
func summarizeBody(r *ghttp.Request) string {
	if r == nil {
		return ""
	}
	if mf := r.Request.MultipartForm; mf != nil {
		return summarizeForm(mf.Value, formFileSizes(mf.File))
	}
	return summarizeRaw(r.GetBody())
}

// summarizeRaw 截断普通请求体并转义为单行；空请求体返回空串。
func summarizeRaw(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) <= maxBodyLogBytes {
		return strconv.Quote(string(body))
	}
	// 截断处按字节切，可能落在多字节字符中间；日志不做完整性要求，如实标注截断即可。
	return strconv.Quote(string(body[:maxBodyLogBytes])) +
		"…（已截断，原始长度 " + strconv.Itoa(len(body)) + " 字节）"
}

// formFileSizes 汇总 multipart 文件字段的文件名与大小，不含文件内容。
func formFileSizes(files map[string][]*multipart.FileHeader) map[string][]string {
	out := make(map[string][]string, len(files))
	for name, headers := range files {
		items := make([]string, 0, len(headers))
		for _, header := range headers {
			items = append(items, header.Filename+"("+strconv.FormatInt(header.Size, 10)+" 字节)")
		}
		out[name] = items
	}
	return out
}

// summarizeForm 把表单字段与文件清单拼成单行摘要，字段按名排序以保证同一请求的日志稳定可比。
//
// 文件只列文件名与大小：调用方要排查的是「哪个字段/哪个文件不对」，不是文件内容本身。
func summarizeForm(values map[string][]string, files map[string][]string) string {
	parts := make([]string, 0, len(values)+len(files))
	for _, name := range sortedKeys(values) {
		shown := make([]string, 0, len(values[name]))
		for _, value := range values[name] {
			shown = append(shown, truncateFormValue(value))
		}
		parts = append(parts, name+"="+strings.Join(shown, "|"))
	}
	for _, name := range sortedKeys(files) {
		parts = append(parts, name+"=[文件 "+strings.Join(files[name], ", ")+"]")
	}
	return strings.Join(parts, " ")
}

// truncateFormValue 截断单个表单字段值，并标注被截断。
func truncateFormValue(value string) string {
	if len(value) <= maxFormValueLogBytes {
		return strconv.Quote(value)
	}
	return strconv.Quote(value[:maxFormValueLogBytes]) +
		"…（已截断，原始长度 " + strconv.Itoa(len(value)) + " 字节）"
}

// sortedKeys 返回按字典序排序的键，让同一请求的日志字段顺序固定。
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
