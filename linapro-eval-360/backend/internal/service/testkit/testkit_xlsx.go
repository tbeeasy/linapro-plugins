// testkit_xlsx.go 提供 xlsx 解析与文件读取辅助（导出断言用）。
// 仅做「xlsx 字节流 → 单元格字符串矩阵」的纯解析，不依赖被测服务。
package testkit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// getenv 读取环境变量并去空白。
func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// readFile 读取 UTF-8 文本文件。
func readFile(path string) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SharedStrings 是 xl/sharedStrings.xml 的解析结构。
type sharedStringsXML struct {
	Items []struct {
		Text     string `xml:"t"`
		RichText []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

// worksheetXML 是 xl/worksheets/sheetN.xml 的解析结构。
type worksheetXML struct {
	SheetData struct {
		Rows []struct {
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline struct {
					Text string `xml:"t"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"row"`
	} `xml:"sheetData"`
}

// parseXLSX 把 xlsx 字节流解析为「按工作表顺序」的字符串矩阵。
// 支持共享字符串表与内联字符串；空行原样保留，便于断言行序。
func parseXLSX(data []byte) ([]*Sheet, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打开 xlsx（zip）失败：%w", err)
	}

	shared, err := readSharedStrings(reader)
	if err != nil {
		return nil, err
	}

	sheets := make([]*Sheet, 0)
	for _, file := range reader.File {
		name := file.Name
		if !strings.HasPrefix(name, "xl/worksheets/sheet") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, err := readZipEntry(file)
		if err != nil {
			return nil, err
		}
		ws := &worksheetXML{}
		if err := xml.Unmarshal(content, ws); err != nil {
			return nil, fmt.Errorf("解析 %s 失败：%w", name, err)
		}
		sheet := &Sheet{Rows: make([][]string, 0, len(ws.SheetData.Rows))}
		for _, row := range ws.SheetData.Rows {
			// 按单元格引用字母定位列号，补空列，保证列序与表头一致
			// （excelize 省略空单元格，紧凑追加会错位）。
			cells := make([]string, 0, len(row.Cells))
			for _, cell := range row.Cells {
				col := columnIndex(cell.Ref)
				for len(cells) <= col {
					cells = append(cells, "")
				}
				cells[col] = cellText(cell.Type, cell.Value, cell.Inline.Text, shared)
			}
			sheet.Rows = append(sheet.Rows, cells)
		}
		if len(sheet.Rows) > 0 {
			sheet.Header = sheet.Rows[0]
		}
		sheets = append(sheets, sheet)
	}
	return sheets, nil
}

// cellText 按单元格类型解析文本：s=共享字符串索引，inlineStr=内联，其余取原始值。
func cellText(cellType, value, inline string, shared []string) string {
	switch cellType {
	case "s":
		idx := -1
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &idx); err != nil {
			return ""
		}
		if idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
		return ""
	case "inlineStr":
		return inline
	default:
		return value
	}
}

// columnIndex 把 A1 风格的单元格引用（如 "C7"）解析为 0-based 列号。
func columnIndex(ref string) int {
	col := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' {
			break
		}
		col = col*26 + int(ch-'A') + 1
	}
	return col - 1
}

// readSharedStrings 读取共享字符串表；缺失时返回空集合。
func readSharedStrings(reader *zip.Reader) ([]string, error) {
	for _, file := range reader.File {
		if file.Name != "xl/sharedStrings.xml" {
			continue
		}
		content, err := readZipEntry(file)
		if err != nil {
			return nil, err
		}
		parsed := &sharedStringsXML{}
		if err := xml.Unmarshal(content, parsed); err != nil {
			return nil, fmt.Errorf("解析 sharedStrings 失败：%w", err)
		}
		out := make([]string, 0, len(parsed.Items))
		for _, item := range parsed.Items {
			if len(item.RichText) > 0 {
				var sb strings.Builder
				for _, run := range item.RichText {
					sb.WriteString(run.Text)
				}
				out = append(out, sb.String())
				continue
			}
			out = append(out, item.Text)
		}
		return out, nil
	}
	return []string{}, nil
}

// readZipEntry 读取单个 zip 条目内容。
func readZipEntry(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开 zip 条目 %s 失败：%w", file.Name, err)
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
