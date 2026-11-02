// bodylimit_read.go 实现规则匹配与按上限读取请求体（multipart 解析到内存、其它换成可重复读取的请求体）。

package bodylimit

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// match 返回第一条路径后缀匹配的规则。
func match(rules []Rule, path string) (Rule, bool) {
	for _, rule := range rules {
		if rule.Limit > 0 && strings.HasSuffix(path, rule.PathSuffix) {
			return rule, true
		}
	}
	return Rule{}, false
}

// readBody 在 limit 内读完请求体：multipart 解析为内存表单，其它内容换成可重复读取的请求体。
// 超限返回 CodeBodyTooLarge，表单无法解析返回 CodeInvalidParam。
func readBody(r *ghttp.Request, limit int64) error {
	if r.Request.ContentLength > limit {
		return tooLarge(limit)
	}
	r.Request.Body = http.MaxBytesReader(r.Response.RawWriter(), r.Request.Body, limit)
	if isMultipart(r.Request.Header.Get("Content-Type")) {
		if err := r.Request.ParseMultipartForm(limit); err != nil {
			if isMaxBytesError(err) {
				return tooLarge(limit)
			}
			return bizerr.NewCode(larkpushcode.CodeInvalidParam,
				bizerr.P("item", "请求体不是合法的 multipart/form-data: "+err.Error()))
		}
		return nil
	}
	body, err := io.ReadAll(r.Request.Body)
	if err != nil {
		if isMaxBytesError(err) {
			return tooLarge(limit)
		}
		return bizerr.NewCode(larkpushcode.CodeInvalidParam, bizerr.P("item", "读取请求体失败: "+err.Error()))
	}
	r.Request.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

// isMultipart 判断 Content-Type 是否为 multipart 表单。
func isMultipart(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.HasPrefix(mediaType, "multipart/")
}

// isMaxBytesError 判断错误是否由请求体超限引起。
func isMaxBytesError(err error) bool {
	var tooLargeErr *http.MaxBytesError
	return errors.As(err, &tooLargeErr)
}

// tooLarge 构造请求体超限错误，limit 以 KB / MB 展示。
func tooLarge(limit int64) error {
	return bizerr.NewCode(larkpushcode.CodeBodyTooLarge, bizerr.P("limit", sizeLabel(limit)))
}

// sizeLabel 把字节数格式化为展示文本：整 MB 显示 MB，否则显示 KB。
func sizeLabel(limit int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
	)
	if limit%mb == 0 {
		return strconv.FormatInt(limit/mb, 10) + " MB"
	}
	return strconv.FormatInt(limit/kb, 10) + " KB"
}
