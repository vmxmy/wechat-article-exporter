package exporter

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultXLSXMaxRows      = 1_048_576 - 1
	defaultXLSXMaxCellBytes = 32_767
)

var ErrXLSXLimit = errors.New("XLSX export limit exceeded")

type XLSXRow struct {
	Account       string
	ArticleID     string
	CanonicalURL  string
	Title         string
	CoverURL      string
	Digest        string
	CreatedAt     time.Time
	PublishedAt   time.Time
	ReadCount     int64
	OldLikeCount  int64
	ShareCount    int64
	LikeCount     int64
	CommentCount  int64
	Author        string
	Original      bool
	MessageType   string
	State         string
	DownloadState string
	Albums        []string
	Content       string
}

type XLSXRowSource interface {
	Next(context.Context) (XLSXRow, error)
}

type XLSXOptions struct {
	IncludeContent bool
	SheetName      string
	MaxRows        int
	MaxCellBytes   int
}

type XLSXReport struct {
	Rows    int `json:"rows"`
	Columns int `json:"columns"`
}

var xlsxBaseColumns = []string{
	"公众号", "ID", "链接", "标题", "封面", "摘要", "创建时间", "发布时间", "阅读", "点赞",
	"分享", "喜欢", "留言", "作者", "是否原创", "文章类型", "状态", "下载状态", "所属合集",
}

func XLSXColumns(includeContent bool) []string {
	columns := append([]string(nil), xlsxBaseColumns...)
	if includeContent {
		columns = append(columns, "文章内容")
	}
	return columns
}

func WriteXLSX(ctx context.Context, destination io.Writer, source XLSXRowSource, options XLSXOptions) (XLSXReport, error) {
	if destination == nil {
		return XLSXReport{}, errors.New("XLSX destination is required")
	}
	if source == nil {
		return XLSXReport{}, errors.New("XLSX row source is required")
	}
	if err := ctx.Err(); err != nil {
		return XLSXReport{}, err
	}
	if options.MaxRows <= 0 {
		options.MaxRows = defaultXLSXMaxRows
	}
	if options.MaxRows > defaultXLSXMaxRows {
		return XLSXReport{}, fmt.Errorf("maximum rows cannot exceed %d: %w", defaultXLSXMaxRows, ErrXLSXLimit)
	}
	if options.MaxCellBytes <= 0 {
		options.MaxCellBytes = defaultXLSXMaxCellBytes
	}
	if options.MaxCellBytes > defaultXLSXMaxCellBytes {
		return XLSXReport{}, fmt.Errorf("maximum cell bytes cannot exceed %d: %w", defaultXLSXMaxCellBytes, ErrXLSXLimit)
	}
	if strings.TrimSpace(options.SheetName) == "" {
		options.SheetName = "Sheet1"
	}
	if err := validateXLSXSheetName(options.SheetName); err != nil {
		return XLSXReport{}, err
	}

	archive := zip.NewWriter(destination)
	failed := true
	defer func() {
		if failed {
			_ = archive.Close()
		}
	}()
	staticParts := xlsxStaticParts(options.SheetName)
	paths := make([]string, 0, len(staticParts))
	for path := range staticParts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := writeXLSXPart(archive, path, staticParts[path]); err != nil {
			return XLSXReport{}, err
		}
	}
	sheet, err := archive.CreateHeader(&zip.FileHeader{Name: "xl/worksheets/sheet1.xml", Method: zip.Deflate})
	if err != nil {
		return XLSXReport{}, fmt.Errorf("create XLSX worksheet: %w", err)
	}
	if _, err := io.WriteString(sheet, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"/></sheetViews><sheetData>`); err != nil {
		return XLSXReport{}, err
	}
	columns := XLSXColumns(options.IncludeContent)
	if err := writeXLSXRow(sheet, 1, columns, options.MaxCellBytes); err != nil {
		return XLSXReport{}, err
	}

	report := XLSXReport{Columns: len(columns)}
	for {
		if err := ctx.Err(); err != nil {
			return XLSXReport{}, err
		}
		row, nextErr := source.Next(ctx)
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return XLSXReport{}, fmt.Errorf("read XLSX row %d: %w", report.Rows+1, nextErr)
		}
		if report.Rows >= options.MaxRows {
			return XLSXReport{}, fmt.Errorf("rows exceed %d: %w", options.MaxRows, ErrXLSXLimit)
		}
		values := xlsxRowValues(row, options.IncludeContent)
		if err := writeXLSXRow(sheet, report.Rows+2, values, options.MaxCellBytes); err != nil {
			return XLSXReport{}, fmt.Errorf("write XLSX row %d: %w", report.Rows+1, err)
		}
		report.Rows++
	}
	if _, err := io.WriteString(sheet, `</sheetData><autoFilter ref="A1:`+xlsxColumnName(len(columns))+strconv.Itoa(report.Rows+1)+`"/><sheetFormatPr defaultRowHeight="15"/></worksheet>`); err != nil {
		return XLSXReport{}, err
	}
	if err := archive.Close(); err != nil {
		return XLSXReport{}, fmt.Errorf("finish XLSX archive: %w", err)
	}
	failed = false
	return report, nil
}

func xlsxRowValues(row XLSXRow, includeContent bool) []string {
	values := []string{
		row.Account, row.ArticleID, row.CanonicalURL, row.Title, row.CoverURL, row.Digest,
		formatXLSXTime(row.CreatedAt), formatXLSXTime(row.PublishedAt),
		strconv.FormatInt(row.ReadCount, 10), strconv.FormatInt(row.OldLikeCount, 10),
		strconv.FormatInt(row.ShareCount, 10), strconv.FormatInt(row.LikeCount, 10), strconv.FormatInt(row.CommentCount, 10),
		row.Author, strconv.FormatBool(row.Original), row.MessageType, row.State, row.DownloadState, strings.Join(row.Albums, "; "),
	}
	if includeContent {
		values = append(values, row.Content)
	}
	return values
}

func writeXLSXRow(writer io.Writer, rowNumber int, values []string, maxCellBytes int) error {
	if _, err := io.WriteString(writer, `<row r="`+strconv.Itoa(rowNumber)+`">`); err != nil {
		return err
	}
	for index, value := range values {
		if len(value) > maxCellBytes {
			return fmt.Errorf("cell %s%d exceeds %d bytes: %w", xlsxColumnName(index+1), rowNumber, maxCellBytes, ErrXLSXLimit)
		}
		if !validXMLText(value) {
			return fmt.Errorf("cell %s%d contains invalid XML text", xlsxColumnName(index+1), rowNumber)
		}
		if _, err := io.WriteString(writer, `<c r="`+xlsxColumnName(index+1)+strconv.Itoa(rowNumber)+`" t="inlineStr"><is><t xml:space="preserve">`); err != nil {
			return err
		}
		if err := xml.EscapeText(writer, []byte(value)); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, `</t></is></c>`); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, `</row>`)
	return err
}

func xlsxStaticParts(sheetName string) map[string]string {
	return map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + xmlEscapeString(sheetName) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"docProps/core.xml":          `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>wechat-article-exporter</dc:creator><dc:title>WeChat articles</dc:title></cp:coreProperties>`,
		"docProps/app.xml":           `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>wechat-article-exporter</Application></Properties>`,
	}
}

func writeXLSXPart(archive *zip.Writer, path, contents string) error {
	writer, err := archive.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("create XLSX part %s: %w", path, err)
	}
	if _, err := io.WriteString(writer, contents); err != nil {
		return fmt.Errorf("write XLSX part %s: %w", path, err)
	}
	return nil
}

func validateXLSXSheetName(value string) error {
	if len([]rune(value)) > 31 || strings.ContainsAny(value, `[]:*?/\`) || strings.HasPrefix(value, "'") || strings.HasSuffix(value, "'") {
		return errors.New("invalid XLSX sheet name")
	}
	return nil
}

func xlsxColumnName(index int) string {
	if index < 1 {
		return ""
	}
	var result []byte
	for index > 0 {
		index--
		result = append([]byte{byte('A' + index%26)}, result...)
		index /= 26
	}
	return string(result)
}

func formatXLSXTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func xmlEscapeString(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func validXMLText(value string) bool {
	for _, character := range value {
		if character == '\t' || character == '\n' || character == '\r' || character >= 0x20 && character <= 0xD7FF || character >= 0xE000 && character <= 0xFFFD || character >= 0x10000 && character <= 0x10FFFF {
			continue
		}
		return false
	}
	return true
}
