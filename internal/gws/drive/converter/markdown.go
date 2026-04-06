package converter

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/gws/model"
)

// Converter converts a document tab to a target format.
type Converter interface {
	Convert(tab model.Tab) string
}

// MarkdownConverter converts Google Docs tab content to markdown.
type MarkdownConverter struct{}

func NewMarkdownConverter() *MarkdownConverter {
	return &MarkdownConverter{}
}

func (c *MarkdownConverter) Convert(tab model.Tab) string {
	var sb strings.Builder
	for _, block := range tab.Body.Content {
		if block.Paragraph != nil {
			c.writeParagraph(&sb, block.Paragraph, tab.Lists)
		} else if block.Table != nil {
			c.writeTable(&sb, block.Table)
		}
	}
	return sb.String()
}

func (c *MarkdownConverter) writeParagraph(sb *strings.Builder, p *model.Paragraph, lists map[string]model.List) {
	text := c.extractText(p)
	if text == "" {
		sb.WriteString("\n")
		return
	}

	style := p.ParagraphStyle.NamedStyleType

	if p.Bullet != nil {
		indent := strings.Repeat("  ", p.Bullet.NestingLevel)
		prefix := "- "
		if c.isOrderedList(p.Bullet, lists) {
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

func (c *MarkdownConverter) extractText(p *model.Paragraph) string {
	var parts []string
	for _, elem := range p.Elements {
		if elem.TextRun == nil {
			continue
		}
		content := strings.TrimRight(elem.TextRun.Content, "\n")
		if content == "" {
			continue
		}
		content = c.applyTextStyle(content, elem.TextRun.TextStyle)
		parts = append(parts, content)
	}
	return strings.Join(parts, "")
}

func (c *MarkdownConverter) applyTextStyle(text string, style model.TextStyle) string {
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

func (c *MarkdownConverter) isOrderedList(bullet *model.Bullet, lists map[string]model.List) bool {
	if lists == nil {
		return false
	}
	list, ok := lists[bullet.ListID]
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

func (c *MarkdownConverter) writeTable(sb *strings.Builder, t *model.Table) {
	if len(t.TableRows) == 0 {
		return
	}

	for ri, row := range t.TableRows {
		sb.WriteString("|")
		for _, cell := range row.TableCells {
			cellText := c.extractCellText(cell)
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

func (c *MarkdownConverter) extractCellText(cell model.TableCell) string {
	var parts []string
	for _, block := range cell.Content {
		if block.Paragraph != nil {
			text := c.extractText(block.Paragraph)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}
