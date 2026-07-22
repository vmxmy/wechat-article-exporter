package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDOCXMaxHTMLBytes  = 16 << 20
	defaultDOCXMaxMediaBytes = 128 << 20
	defaultDOCXMaxMediaCount = 10_000
	defaultDOCXMaxXMLBytes   = 32 << 20
)

var ErrDOCXLimit = errors.New("DOCX export limit exceeded")

type DOCXDocument struct {
	Title       string
	Account     string
	Author      string
	PublishedAt time.Time
	HTML        string
	Media       []DOCXMedia
	Comments    []DOCXComment
}

type DOCXMedia struct {
	Source      string
	Name        string
	ContentType string
	Data        []byte
}

type DOCXComment struct {
	Author    string
	Content   string
	CreatedAt time.Time
	Replies   []DOCXReply
}

type DOCXReply struct {
	Author    string
	Content   string
	CreatedAt time.Time
}

type DOCXOptions struct {
	IncludeComments bool
	MaxHTMLBytes    int
	MaxMediaBytes   int64
	MaxMediaCount   int
}

type DOCXReport struct {
	Paragraphs    int `json:"paragraphs"`
	Tables        int `json:"tables"`
	Hyperlinks    int `json:"hyperlinks"`
	MediaEmbedded int `json:"mediaEmbedded"`
	Comments      int `json:"comments"`
}

type DOCXValidationOptions struct {
	MaxPackageBytes int64
	MaxEntries      int
	MaxXMLBytes     int64
}

type DOCXValidationReport struct {
	Valid      bool     `json:"valid"`
	Entries    int      `json:"entries"`
	Paragraphs int      `json:"paragraphs"`
	Tables     int      `json:"tables"`
	Hyperlinks int      `json:"hyperlinks"`
	Media      int      `json:"media"`
	Issues     []string `json:"issues,omitempty"`
}

type docxRelationship struct {
	ID       string
	Type     string
	Target   string
	External bool
}

type docxMediaPart struct {
	DOCXMedia
	Path string
	ID   string
}

func WriteDOCX(ctx context.Context, destination io.Writer, document DOCXDocument, options DOCXOptions) (DOCXReport, error) {
	if destination == nil {
		return DOCXReport{}, errors.New("DOCX destination is required")
	}
	if err := ctx.Err(); err != nil {
		return DOCXReport{}, err
	}
	if strings.TrimSpace(document.Title) == "" {
		return DOCXReport{}, errors.New("DOCX title is required")
	}
	if options.MaxHTMLBytes <= 0 {
		options.MaxHTMLBytes = defaultDOCXMaxHTMLBytes
	}
	if options.MaxMediaBytes <= 0 {
		options.MaxMediaBytes = defaultDOCXMaxMediaBytes
	}
	if options.MaxMediaCount <= 0 {
		options.MaxMediaCount = defaultDOCXMaxMediaCount
	}
	if len(document.HTML) > options.MaxHTMLBytes {
		return DOCXReport{}, fmt.Errorf("HTML exceeds %d bytes: %w", options.MaxHTMLBytes, ErrDOCXLimit)
	}
	media, mediaBySource, err := prepareDOCXMedia(document.Media, options)
	if err != nil {
		return DOCXReport{}, err
	}
	root, err := parseDOCXHTML(document.HTML)
	if err != nil {
		return DOCXReport{}, err
	}
	renderer := newDOCXRenderer(mediaBySource)
	body, err := renderer.renderDocument(ctx, document, root, options.IncludeComments)
	if err != nil {
		return DOCXReport{}, err
	}

	archive := zip.NewWriter(destination)
	failed := true
	defer func() {
		if failed {
			_ = archive.Close()
		}
	}()
	parts := docxStaticParts(document, media, renderer.relationships)
	parts["word/document.xml"] = body
	paths := make([]string, 0, len(parts))
	for path := range parts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := writeDOCXPart(archive, path, []byte(parts[path])); err != nil {
			return DOCXReport{}, err
		}
	}
	for _, part := range media {
		if err := ctx.Err(); err != nil {
			return DOCXReport{}, err
		}
		if err := writeDOCXPart(archive, part.Path, part.Data); err != nil {
			return DOCXReport{}, err
		}
	}
	if err := archive.Close(); err != nil {
		return DOCXReport{}, fmt.Errorf("finish DOCX archive: %w", err)
	}
	failed = false
	renderer.report.MediaEmbedded = len(media)
	if options.IncludeComments {
		renderer.report.Comments = len(document.Comments)
	}
	return renderer.report, nil
}

func ValidateDOCX(readerAt io.ReaderAt, size int64, options DOCXValidationOptions) (DOCXValidationReport, error) {
	report := DOCXValidationReport{Issues: []string{}}
	if readerAt == nil || size <= 0 {
		return report, errors.New("DOCX package is empty")
	}
	if options.MaxPackageBytes <= 0 {
		options.MaxPackageBytes = 512 << 20
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = 20_000
	}
	if options.MaxXMLBytes <= 0 {
		options.MaxXMLBytes = defaultDOCXMaxXMLBytes
	}
	if size > options.MaxPackageBytes {
		return report, fmt.Errorf("DOCX package exceeds %d bytes: %w", options.MaxPackageBytes, ErrDOCXLimit)
	}
	archive, err := zip.NewReader(readerAt, size)
	if err != nil {
		return report, fmt.Errorf("open DOCX package: %w", err)
	}
	if len(archive.File) > options.MaxEntries {
		return report, fmt.Errorf("DOCX entries exceed %d: %w", options.MaxEntries, ErrDOCXLimit)
	}
	report.Entries = len(archive.File)
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if !safeDOCXPartName(file.Name) {
			report.Issues = append(report.Issues, "unsafe package path: "+file.Name)
			continue
		}
		if _, exists := files[file.Name]; exists {
			report.Issues = append(report.Issues, "duplicate package path: "+file.Name)
			continue
		}
		files[file.Name] = file
		if strings.HasPrefix(file.Name, "word/media/") {
			report.Media++
		}
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if files[required] == nil {
			report.Issues = append(report.Issues, "missing package part: "+required)
		}
	}
	documentXML := ""
	if file := files["word/document.xml"]; file != nil {
		contents, readErr := readDOCXZipPart(file, options.MaxXMLBytes)
		if readErr != nil {
			return report, readErr
		}
		documentXML = string(contents)
		if err := validateXML(contents); err != nil {
			report.Issues = append(report.Issues, "invalid word/document.xml: "+err.Error())
		}
		report.Paragraphs = strings.Count(documentXML, "<w:p") - strings.Count(documentXML, "<w:pPr")
		report.Tables = strings.Count(documentXML, "<w:tbl>")
		report.Hyperlinks = strings.Count(documentXML, "<w:hyperlink")
	}
	for _, path := range []string{"[Content_Types].xml", "_rels/.rels", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if file := files[path]; file != nil {
			contents, readErr := readDOCXZipPart(file, options.MaxXMLBytes)
			if readErr != nil {
				return report, readErr
			}
			if err := validateXML(contents); err != nil {
				report.Issues = append(report.Issues, "invalid "+path+": "+err.Error())
			}
		}
	}
	if file := files["word/_rels/document.xml.rels"]; file != nil {
		contents, readErr := readDOCXZipPart(file, options.MaxXMLBytes)
		if readErr != nil {
			return report, readErr
		}
		for _, target := range relationshipTargets(contents) {
			if strings.HasPrefix(target, "media/") && files["word/"+target] == nil {
				report.Issues = append(report.Issues, "missing relationship target: word/"+target)
			}
		}
		for _, required := range []struct {
			typeSuffix string
			target     string
		}{
			{typeSuffix: "/styles", target: "styles.xml"},
			{typeSuffix: "/numbering", target: "numbering.xml"},
		} {
			if !hasDOCXRelationship(contents, required.typeSuffix, required.target) {
				report.Issues = append(report.Issues, "missing document relationship: "+required.target)
			}
		}
	}
	report.Valid = len(report.Issues) == 0
	if !report.Valid {
		return report, errors.New(strings.Join(report.Issues, "; "))
	}
	return report, nil
}

func prepareDOCXMedia(values []DOCXMedia, options DOCXOptions) ([]docxMediaPart, map[string]docxMediaPart, error) {
	if len(values) > options.MaxMediaCount {
		return nil, nil, fmt.Errorf("media items exceed %d: %w", options.MaxMediaCount, ErrDOCXLimit)
	}
	parts := make([]docxMediaPart, 0, len(values))
	bySource := make(map[string]docxMediaPart, len(values))
	usedNames := make(map[string]struct{})
	var total int64
	for index, value := range values {
		total += int64(len(value.Data))
		if total > options.MaxMediaBytes {
			return nil, nil, fmt.Errorf("media bytes exceed %d: %w", options.MaxMediaBytes, ErrDOCXLimit)
		}
		if len(value.Data) == 0 || strings.TrimSpace(value.Source) == "" {
			return nil, nil, fmt.Errorf("DOCX media %d is incomplete", index)
		}
		name := sanitizeDOCXMediaName(value.Name, index)
		for attempt := 2; ; attempt++ {
			if _, exists := usedNames[name]; !exists {
				break
			}
			extension := filepath.Ext(name)
			base := strings.TrimSuffix(name, extension)
			name = base + "-" + strconv.Itoa(attempt) + extension
		}
		usedNames[name] = struct{}{}
		part := docxMediaPart{DOCXMedia: value, Path: "word/media/" + name, ID: "rIdMedia" + strconv.Itoa(index+1)}
		if strings.TrimSpace(part.ContentType) == "" {
			part.ContentType = docxContentType(name)
		}
		parts = append(parts, part)
		bySource[value.Source] = part
	}
	return parts, bySource, nil
}

func sanitizeDOCXMediaName(value string, index int) string {
	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if name == "." || name == "" {
		name = "media-" + strconv.Itoa(index+1) + ".bin"
	}
	var builder strings.Builder
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

type docxRenderer struct {
	media         map[string]docxMediaPart
	relationships []docxRelationship
	linkIDs       map[string]string
	report        DOCXReport
	drawingID     int
}

func newDOCXRenderer(media map[string]docxMediaPart) *docxRenderer {
	renderer := &docxRenderer{media: media, linkIDs: make(map[string]string)}
	renderer.relationships = append(renderer.relationships,
		docxRelationship{
			ID: "rIdStyles", Type: "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles", Target: "styles.xml",
		},
		docxRelationship{
			ID: "rIdNumbering", Type: "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering", Target: "numbering.xml",
		},
	)
	parts := make([]docxMediaPart, 0, len(media))
	seen := make(map[string]struct{})
	for _, part := range media {
		if _, exists := seen[part.ID]; exists {
			continue
		}
		seen[part.ID] = struct{}{}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(left, right int) bool { return parts[left].ID < parts[right].ID })
	for _, part := range parts {
		renderer.relationships = append(renderer.relationships, docxRelationship{
			ID: part.ID, Type: "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", Target: strings.TrimPrefix(part.Path, "word/"),
		})
	}
	return renderer
}

func (renderer *docxRenderer) renderDocument(ctx context.Context, document DOCXDocument, root *docxHTMLNode, includeComments bool) (string, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)
	body.WriteString(docxParagraph(document.Title, "Title", ""))
	renderer.report.Paragraphs++
	metadata := docxMetadata(document)
	if metadata != "" {
		body.WriteString(docxParagraph(metadata, "Subtitle", ""))
		renderer.report.Paragraphs++
	}
	if err := renderer.renderChildren(ctx, &body, root, 0); err != nil {
		return "", err
	}
	if includeComments && len(document.Comments) > 0 {
		body.WriteString(docxParagraph("Comments", "Heading2", ""))
		renderer.report.Paragraphs++
		for _, comment := range document.Comments {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			body.WriteString(docxParagraph(comment.Author, "Heading3", ""))
			body.WriteString(docxParagraph(comment.Content, "Normal", ""))
			renderer.report.Paragraphs += 2
			for _, reply := range comment.Replies {
				body.WriteString(docxParagraph(reply.Author+": "+reply.Content, "CommentReply", ""))
				renderer.report.Paragraphs++
			}
		}
	}
	body.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`)
	return body.String(), nil
}

func (renderer *docxRenderer) renderChildren(ctx context.Context, output *strings.Builder, node *docxHTMLNode, listLevel int) error {
	for _, child := range node.Children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if child.Type == docxHTMLText {
			if text := compactDOCXText(child.Text); text != "" {
				output.WriteString(docxParagraph(text, "Normal", ""))
				renderer.report.Paragraphs++
			}
			continue
		}
		switch child.Tag {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level, _ := strconv.Atoi(strings.TrimPrefix(child.Tag, "h"))
			output.WriteString(docxParagraph(compactDOCXText(docxNodeText(child)), "Heading"+strconv.Itoa(level), ""))
			renderer.report.Paragraphs++
		case "p", "div", "section", "article", "main", "figcaption":
			if renderer.nodeContainsBlock(child) {
				if err := renderer.renderChildren(ctx, output, child, listLevel); err != nil {
					return err
				}
			} else {
				output.WriteString(renderer.renderInlineParagraph(child, "Normal", ""))
				renderer.report.Paragraphs++
			}
		case "blockquote":
			output.WriteString(renderer.renderInlineParagraph(child, "Quote", ""))
			renderer.report.Paragraphs++
		case "pre":
			output.WriteString(docxParagraph(docxNodeText(child), "CodeBlock", ""))
			renderer.report.Paragraphs++
		case "ul":
			if err := renderer.renderList(ctx, output, child, listLevel, 1); err != nil {
				return err
			}
		case "ol":
			if err := renderer.renderList(ctx, output, child, listLevel, 2); err != nil {
				return err
			}
		case "table":
			output.WriteString(renderer.renderTable(child))
			renderer.report.Tables++
		case "img":
			if drawing := renderer.renderImage(child); drawing != "" {
				output.WriteString(drawing)
				renderer.report.Paragraphs++
			}
		case "audio":
			output.WriteString(docxParagraph("[Audio] "+child.Attr["src"], "MediaReference", ""))
			renderer.report.Paragraphs++
		case "video":
			output.WriteString(docxParagraph("[Video] "+child.Attr["src"], "MediaReference", ""))
			renderer.report.Paragraphs++
		case "script", "style", "iframe", "object", "embed":
		default:
			if err := renderer.renderChildren(ctx, output, child, listLevel); err != nil {
				return err
			}
		}
	}
	return nil
}

func (renderer *docxRenderer) renderInlineParagraph(node *docxHTMLNode, style, numbering string) string {
	var runs strings.Builder
	renderer.renderInline(&runs, node, docxRunStyle{})
	return docxParagraphXML(runs.String(), style, numbering)
}

type docxRunStyle struct {
	Bold   bool
	Italic bool
	Code   bool
}

func (renderer *docxRenderer) renderInline(output *strings.Builder, node *docxHTMLNode, style docxRunStyle) {
	for _, child := range node.Children {
		if child.Type == docxHTMLText {
			output.WriteString(docxRun(child.Text, style))
			continue
		}
		next := style
		switch child.Tag {
		case "strong", "b":
			next.Bold = true
		case "em", "i":
			next.Italic = true
		case "code", "kbd", "samp":
			next.Code = true
		case "br":
			output.WriteString(`<w:r><w:br/></w:r>`)
			continue
		case "a":
			href := strings.TrimSpace(child.Attr["href"])
			if href != "" && safeDOCXHyperlink(href) {
				id := renderer.hyperlinkRelationship(href)
				output.WriteString(`<w:hyperlink r:id="` + id + `" w:history="1">`)
				renderer.renderInline(output, child, next)
				output.WriteString(`</w:hyperlink>`)
				renderer.report.Hyperlinks++
				continue
			}
		case "img":
			if drawing := renderer.renderImage(child); drawing != "" {
				output.WriteString(strings.TrimPrefix(strings.TrimSuffix(drawing, "</w:p>"), `<w:p>`))
			}
			continue
		case "audio":
			output.WriteString(docxRun("[Audio] "+child.Attr["src"], next))
			continue
		case "video":
			output.WriteString(docxRun("[Video] "+child.Attr["src"], next))
			continue
		}
		renderer.renderInline(output, child, next)
	}
}

func (renderer *docxRenderer) renderList(ctx context.Context, output *strings.Builder, node *docxHTMLNode, level, numberID int) error {
	for _, child := range node.Children {
		if child.Type != docxHTMLElement || child.Tag != "li" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		item := &docxHTMLNode{Type: docxHTMLElement, Tag: "span"}
		var nested []*docxHTMLNode
		for _, content := range child.Children {
			if content.Type == docxHTMLElement && (content.Tag == "ul" || content.Tag == "ol") {
				nested = append(nested, content)
			} else {
				item.Children = append(item.Children, content)
			}
		}
		output.WriteString(renderer.renderInlineParagraph(item, "ListParagraph", strconv.Itoa(level)+":"+strconv.Itoa(numberID)))
		renderer.report.Paragraphs++
		for _, nestedList := range nested {
			nestedNumberID := 1
			if nestedList.Tag == "ol" {
				nestedNumberID = 2
			}
			if err := renderer.renderList(ctx, output, nestedList, level+1, nestedNumberID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (renderer *docxRenderer) renderTable(node *docxHTMLNode) string {
	var builder strings.Builder
	builder.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr>`)
	for _, row := range docxDescendants(node, "tr") {
		builder.WriteString(`<w:tr>`)
		for _, cell := range row.Children {
			if cell.Type != docxHTMLElement || cell.Tag != "td" && cell.Tag != "th" {
				continue
			}
			style := "Normal"
			if cell.Tag == "th" {
				style = "TableHeading"
			}
			builder.WriteString(`<w:tc><w:tcPr/>`)
			builder.WriteString(renderer.renderInlineParagraph(cell, style, ""))
			builder.WriteString(`</w:tc>`)
		}
		builder.WriteString(`</w:tr>`)
	}
	builder.WriteString(`</w:tbl>`)
	return builder.String()
}

func (renderer *docxRenderer) renderImage(node *docxHTMLNode) string {
	part, exists := renderer.media[node.Attr["src"]]
	if !exists {
		alt := compactDOCXText(node.Attr["alt"])
		if alt == "" {
			alt = "image"
		}
		return docxParagraph("[Image: "+alt+"]", "MediaReference", "")
	}
	renderer.drawingID++
	name := xmlEscapeString(filepath.Base(part.Path))
	return `<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="5486400" cy="3086100"/><wp:docPr id="` + strconv.Itoa(renderer.drawingID) + `" name="` + name + `"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="` + name + `"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="` + part.ID + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="5486400" cy="3086100"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
}

func (renderer *docxRenderer) hyperlinkRelationship(target string) string {
	if id := renderer.linkIDs[target]; id != "" {
		return id
	}
	id := "rIdLink" + strconv.Itoa(len(renderer.linkIDs)+1)
	renderer.linkIDs[target] = id
	renderer.relationships = append(renderer.relationships, docxRelationship{ID: id, Type: "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink", Target: target, External: true})
	return id
}

func (renderer *docxRenderer) nodeContainsBlock(node *docxHTMLNode) bool {
	for _, child := range node.Children {
		if child.Type != docxHTMLElement {
			continue
		}
		switch child.Tag {
		case "h1", "h2", "h3", "h4", "h5", "h6", "p", "div", "section", "article", "blockquote", "pre", "ul", "ol", "table":
			return true
		}
	}
	return false
}

func docxStaticParts(document DOCXDocument, media []docxMediaPart, relationships []docxRelationship) map[string]string {
	contentTypes := map[string]string{
		"rels": "application/vnd.openxmlformats-package.relationships+xml",
		"xml":  "application/xml",
	}
	for _, part := range media {
		extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(part.Path)), ".")
		if extension != "" {
			contentTypes[extension] = part.ContentType
		}
	}
	var defaults strings.Builder
	keys := make([]string, 0, len(contentTypes))
	for key := range contentTypes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		defaults.WriteString(`<Default Extension="` + xmlEscapeString(key) + `" ContentType="` + xmlEscapeString(contentTypes[key]) + `"/>`)
	}
	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, relationship := range relationships {
		rels.WriteString(`<Relationship Id="` + xmlEscapeString(relationship.ID) + `" Type="` + xmlEscapeString(relationship.Type) + `" Target="` + xmlEscapeString(relationship.Target) + `"`)
		if relationship.External {
			rels.WriteString(` TargetMode="External"`)
		}
		rels.WriteString(`/>`)
	}
	rels.WriteString(`</Relationships>`)
	return map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + defaults.String() + `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": rels.String(),
		"word/styles.xml":              docxStyles,
		"word/numbering.xml":           docxNumbering,
		"docProps/core.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"><dc:title>` + xmlEscapeString(document.Title) + `</dc:title><dc:creator>` + xmlEscapeString(document.Author) + `</dc:creator></cp:coreProperties>`,
		"docProps/app.xml":             `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>wechat-article-exporter</Application></Properties>`,
	}
}

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:rPr><w:color w:val="666666"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Heading5"><w:name w:val="heading 5"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Heading6"><w:name w:val="heading 6"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/></w:pPr></w:style><w:style w:type="paragraph" w:styleId="CodeBlock"><w:name w:val="Code Block"/><w:basedOn w:val="Normal"/><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="MediaReference"><w:name w:val="Media Reference"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="CommentReply"><w:name w:val="Comment Reply"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/></w:pPr></w:style><w:style w:type="paragraph" w:styleId="TableHeading"><w:name w:val="Table Heading"/><w:basedOn w:val="Normal"/><w:rPr><w:b/></w:rPr></w:style><w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/></w:style></w:styles>`

const docxNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/><w:lvlText w:val="◦"/></w:lvl></w:abstractNum><w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="multilevel"/><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num></w:numbering>`

func docxParagraph(value, style, numbering string) string {
	return docxParagraphXML(docxRun(value, docxRunStyle{}), style, numbering)
}

func docxParagraphXML(runs, style, numbering string) string {
	var properties strings.Builder
	if style != "" {
		properties.WriteString(`<w:pStyle w:val="` + xmlEscapeString(style) + `"/>`)
	}
	if numbering != "" {
		parts := strings.SplitN(numbering, ":", 2)
		properties.WriteString(`<w:numPr><w:ilvl w:val="` + xmlEscapeString(parts[0]) + `"/><w:numId w:val="` + xmlEscapeString(parts[1]) + `"/></w:numPr>`)
	}
	return `<w:p><w:pPr>` + properties.String() + `</w:pPr>` + runs + `</w:p>`
}

func docxRun(value string, style docxRunStyle) string {
	if value == "" {
		return ""
	}
	var properties strings.Builder
	if style.Bold {
		properties.WriteString(`<w:b/>`)
	}
	if style.Italic {
		properties.WriteString(`<w:i/>`)
	}
	if style.Code {
		properties.WriteString(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`)
	}
	return `<w:r><w:rPr>` + properties.String() + `</w:rPr><w:t xml:space="preserve">` + xmlEscapeString(value) + `</w:t></w:r>`
}

func docxMetadata(document DOCXDocument) string {
	parts := make([]string, 0, 3)
	if document.Account != "" {
		parts = append(parts, document.Account)
	}
	if document.Author != "" && document.Author != document.Account {
		parts = append(parts, document.Author)
	}
	if !document.PublishedAt.IsZero() {
		parts = append(parts, document.PublishedAt.UTC().Format("2006-01-02"))
	}
	return strings.Join(parts, " · ")
}

func writeDOCXPart(archive *zip.Writer, path string, contents []byte) error {
	writer, err := archive.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("create DOCX part %s: %w", path, err)
	}
	if _, err := writer.Write(contents); err != nil {
		return fmt.Errorf("write DOCX part %s: %w", path, err)
	}
	return nil
}

func safeDOCXPartName(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean == value && value != "." && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../") && !strings.Contains(value, "\\")
}

func readDOCXZipPart(file *zip.File, maximum int64) ([]byte, error) {
	if int64(file.UncompressedSize64) > maximum {
		return nil, fmt.Errorf("DOCX part %s exceeds %d bytes: %w", file.Name, maximum, ErrDOCXLimit)
	}
	opened, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	contents, err := io.ReadAll(io.LimitReader(opened, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("DOCX part %s exceeds %d bytes: %w", file.Name, maximum, ErrDOCXLimit)
	}
	return contents, nil
}

func validateXML(contents []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func relationshipTargets(contents []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	var targets []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		for _, attribute := range start.Attr {
			if attribute.Name.Local == "Target" {
				targets = append(targets, attribute.Value)
			}
		}
	}
	return targets
}

func hasDOCXRelationship(contents []byte, typeSuffix, target string) bool {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var relationshipType, relationshipTarget string
		for _, attribute := range start.Attr {
			switch attribute.Name.Local {
			case "Type":
				relationshipType = attribute.Value
			case "Target":
				relationshipTarget = attribute.Value
			}
		}
		if strings.HasSuffix(relationshipType, typeSuffix) && relationshipTarget == target {
			return true
		}
	}
}

func safeDOCXHyperlink(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:")
}

func docxContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func compactDOCXText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

type docxHTMLNodeType uint8

const (
	docxHTMLDocument docxHTMLNodeType = iota
	docxHTMLElement
	docxHTMLText
)

type docxHTMLNode struct {
	Type     docxHTMLNodeType
	Tag      string
	Attr     map[string]string
	Text     string
	Children []*docxHTMLNode
	Parent   *docxHTMLNode
}

var docxVoidTags = map[string]bool{"br": true, "hr": true, "img": true, "source": true, "meta": true, "link": true}

func parseDOCXHTML(value string) (*docxHTMLNode, error) {
	root := &docxHTMLNode{Type: docxHTMLDocument}
	current := root
	lower := strings.ToLower(value)
	position := 0
	nodes := 1
	for position < len(value) {
		if nodes > 250_000 {
			return nil, fmt.Errorf("HTML nodes exceed 250000: %w", ErrDOCXLimit)
		}
		if value[position] != '<' {
			next := strings.IndexByte(value[position:], '<')
			if next < 0 {
				next = len(value) - position
			}
			text := value[position : position+next]
			if text != "" {
				appendDOCXNode(current, &docxHTMLNode{Type: docxHTMLText, Text: htmlEntityDecode(text)})
				nodes++
			}
			position += next
			continue
		}
		if strings.HasPrefix(value[position:], "<!--") {
			end := strings.Index(value[position+4:], "-->")
			if end < 0 {
				break
			}
			position += end + 7
			continue
		}
		end := docxTagEnd(value, position+1)
		if end < 0 {
			appendDOCXNode(current, &docxHTMLNode{Type: docxHTMLText, Text: htmlEntityDecode(value[position:])})
			break
		}
		raw := strings.TrimSpace(value[position+1 : end])
		if raw == "" || strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "?") {
			position = end + 1
			continue
		}
		if raw[0] == '/' {
			tag := strings.ToLower(strings.Fields(strings.TrimSpace(raw[1:]))[0])
			for node := current; node != nil && node != root; node = node.Parent {
				if node.Tag == tag {
					current = node.Parent
					break
				}
			}
			position = end + 1
			continue
		}
		selfClosing := strings.HasSuffix(raw, "/")
		if selfClosing {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, "/"))
		}
		tag, attrs := parseDOCXTag(raw)
		if tag == "" {
			position = end + 1
			continue
		}
		node := &docxHTMLNode{Type: docxHTMLElement, Tag: tag, Attr: attrs}
		appendDOCXNode(current, node)
		nodes++
		position = end + 1
		if tag == "script" || tag == "style" {
			closeTag := "</" + tag
			relative := strings.Index(lower[position:], closeTag)
			if relative < 0 {
				break
			}
			closeStart := position + relative
			closeEnd := docxTagEnd(value, closeStart+len(closeTag))
			if closeEnd < 0 {
				break
			}
			position = closeEnd + 1
			continue
		}
		if !selfClosing && !docxVoidTags[tag] {
			depth := 0
			for ancestor := node; ancestor != nil && ancestor != root; ancestor = ancestor.Parent {
				depth++
			}
			if depth > 256 {
				return nil, fmt.Errorf("HTML nesting exceeds 256: %w", ErrDOCXLimit)
			}
			current = node
		}
	}
	return root, nil
}

func parseDOCXTag(raw string) (string, map[string]string) {
	position := 0
	for position < len(raw) && docxSpace(raw[position]) {
		position++
	}
	start := position
	for position < len(raw) && docxNameChar(raw[position]) {
		position++
	}
	if start == position {
		return "", nil
	}
	tag := strings.ToLower(raw[start:position])
	attrs := make(map[string]string)
	for position < len(raw) {
		for position < len(raw) && docxSpace(raw[position]) {
			position++
		}
		start = position
		for position < len(raw) && !docxSpace(raw[position]) && raw[position] != '=' {
			position++
		}
		if start == position {
			position++
			continue
		}
		name := strings.ToLower(raw[start:position])
		for position < len(raw) && docxSpace(raw[position]) {
			position++
		}
		value := ""
		if position < len(raw) && raw[position] == '=' {
			position++
			for position < len(raw) && docxSpace(raw[position]) {
				position++
			}
			if position < len(raw) && (raw[position] == '\'' || raw[position] == '"') {
				quote := raw[position]
				position++
				start = position
				for position < len(raw) && raw[position] != quote {
					position++
				}
				value = raw[start:position]
				if position < len(raw) {
					position++
				}
			} else {
				start = position
				for position < len(raw) && !docxSpace(raw[position]) {
					position++
				}
				value = raw[start:position]
			}
		}
		attrs[name] = htmlEntityDecode(value)
	}
	return tag, attrs
}

func docxTagEnd(value string, position int) int {
	var quote byte
	for index := position; index < len(value); index++ {
		char := value[index]
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '>' {
			return index
		}
	}
	return -1
}

func appendDOCXNode(parent, child *docxHTMLNode) {
	child.Parent = parent
	parent.Children = append(parent.Children, child)
}

func docxNodeText(node *docxHTMLNode) string {
	if node.Type == docxHTMLText {
		return node.Text
	}
	var builder strings.Builder
	for _, child := range node.Children {
		builder.WriteString(docxNodeText(child))
	}
	return builder.String()
}

func docxDescendants(node *docxHTMLNode, tag string) []*docxHTMLNode {
	var result []*docxHTMLNode
	var walk func(*docxHTMLNode)
	walk = func(current *docxHTMLNode) {
		if current.Type == docxHTMLElement && current.Tag == tag {
			result = append(result, current)
			return
		}
		for _, child := range current.Children {
			walk(child)
		}
	}
	walk(node)
	return result
}

func docxSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n' || character == '\f'
}

func docxNameChar(character byte) bool {
	return character == '-' || character == ':' || character == '_' || character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func htmlEntityDecode(value string) string {
	return stdhtml.UnescapeString(value)
}
