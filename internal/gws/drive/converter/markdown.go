package converter

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/gws/model"
)

// MarkdownConverter converts Google Docs tab content to markdown.
type MarkdownConverter struct{}

func NewMarkdownConverter() *MarkdownConverter {
	return &MarkdownConverter{}
}

// ImageRef is an inline image found during conversion. The caller is
// responsible for downloading the image and storing it at the local path.
type ImageRef struct {
	ObjectID string // Google Docs inline object ID
	ImageURI string // remote content URI (requires auth)
	Filename string // suggested local filename (e.g. "img-kABCDEFG.png")
}

// ConvertResult holds the markdown output and any images referenced in it.
type ConvertResult struct {
	Markdown string
	Images   []ImageRef
}

// Convert renders a tab as markdown and collects inline image references.
// The caller is responsible for downloading images listed in Images.
func (c *MarkdownConverter) Convert(tab model.Tab) ConvertResult {
	ctx := &convertContext{
		lists:         tab.Lists,
		inlineObjects: tab.InlineObjects,
	}

	var sb strings.Builder
	for _, block := range tab.Body.Content {
		if block.Paragraph != nil {
			ctx.writeParagraph(&sb, block.Paragraph)
		} else if block.Table != nil {
			ctx.writeTable(&sb, block.Table)
		}
	}
	return ConvertResult{Markdown: sb.String(), Images: ctx.images}
}

// convertContext carries per-tab state through the conversion.
type convertContext struct {
	lists         map[string]model.List
	inlineObjects map[string]model.InlineObject
	images        []ImageRef
}

func (ctx *convertContext) writeParagraph(sb *strings.Builder, p *model.Paragraph) {
	text := ctx.extractText(p)
	if text == "" {
		sb.WriteString("\n")
		return
	}

	style := p.ParagraphStyle.NamedStyleType

	if p.Bullet != nil {
		indent := strings.Repeat("  ", p.Bullet.NestingLevel)
		prefix := "- "
		if ctx.isOrderedList(p.Bullet) {
			prefix = "1. "
		}
		sb.WriteString(indent + prefix + text + "\n")
		return
	}

	switch style {
	case "HEADING_1":
		sb.WriteString("# " + text + "\n\n")
	case "HEADING_2":
		sb.WriteString("## " + text + "\n\n")
	case "HEADING_3":
		sb.WriteString("### " + text + "\n\n")
	case "HEADING_4":
		sb.WriteString("#### " + text + "\n\n")
	case "HEADING_5":
		sb.WriteString("##### " + text + "\n\n")
	case "HEADING_6":
		sb.WriteString("###### " + text + "\n\n")
	default:
		sb.WriteString(text + "\n\n")
	}
}

func (ctx *convertContext) extractText(p *model.Paragraph) string {
	var parts []string
	for _, elem := range p.Elements {
		if elem.TextRun != nil {
			content := strings.TrimRight(elem.TextRun.Content, "\n")
			if content == "" {
				continue
			}
			content = applyTextStyle(content, elem.TextRun.TextStyle)
			parts = append(parts, content)
		} else if elem.InlineObjectElement != nil {
			ref := ctx.renderInlineObject(elem.InlineObjectElement.InlineObjectID)
			if ref != "" {
				parts = append(parts, ref)
			}
		}
	}
	return strings.Join(parts, "")
}

// renderInlineObject returns a markdown image reference and records the
// image for later download. Returns empty string if the object is unknown
// or has no image URI.
func (ctx *convertContext) renderInlineObject(objectID string) string {
	obj, ok := ctx.inlineObjects[objectID]
	if !ok || obj.ImageURI == "" {
		return ""
	}

	filename := fmt.Sprintf("img-%s.png", objectID)
	if len(filename) > 40 {
		filename = fmt.Sprintf("img-%s.png", objectID[:20])
	}

	ctx.images = append(ctx.images, ImageRef{
		ObjectID: objectID,
		ImageURI: obj.ImageURI,
		Filename: filename,
	})

	alt := obj.Title
	if alt == "" {
		alt = "image"
	}
	return fmt.Sprintf("![%s](attachments/%s)", alt, filename)
}

func applyTextStyle(text string, style model.TextStyle) string {
	if style.Link != nil && style.Link.URL != "" {
		text = fmt.Sprintf("[%s](%s)", text, style.Link.URL)
	}
	if style.Bold {
		text = "**" + text + "**"
	}
	if style.Italic {
		text = "_" + text + "_"
	}
	if style.Strikethrough {
		text = "~~" + text + "~~"
	}
	return text
}

func (ctx *convertContext) isOrderedList(bullet *model.Bullet) bool {
	if ctx.lists == nil {
		return false
	}
	list, ok := ctx.lists[bullet.ListID]
	if !ok {
		return false
	}
	levels := list.ListProperties.NestingLevels
	if bullet.NestingLevel >= len(levels) {
		return false
	}
	gl := levels[bullet.NestingLevel].GlyphType
	return gl == "DECIMAL" || gl == "ALPHA" || gl == "ROMAN"
}

func (ctx *convertContext) writeTable(sb *strings.Builder, t *model.Table) {
	if len(t.TableRows) == 0 {
		return
	}

	for ri, row := range t.TableRows {
		sb.WriteString("|")
		for _, cell := range row.TableCells {
			cellText := ctx.extractCellText(cell)
			sb.WriteString(" " + cellText + " |")
		}
		sb.WriteString("\n")

		if ri == 0 {
			sb.WriteString("|")
			for range row.TableCells {
				sb.WriteString(" --- |")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

func (ctx *convertContext) extractCellText(cell model.TableCell) string {
	var parts []string
	for _, block := range cell.Content {
		if block.Paragraph != nil {
			text := ctx.extractText(block.Paragraph)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}
