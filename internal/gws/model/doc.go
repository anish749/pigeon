package model

import "encoding/json"

// Document represents a Google Docs document response.
type Document struct {
	DocumentID string   `json:"documentId"`
	Title      string   `json:"title"`
	Tabs       []RawTab `json:"tabs"`
}

// Tab is the flattened representation of a document tab with its content.
type Tab struct {
	Title string
	TabID string
	Body  Body
	Lists map[string]List
}

// RawTab mirrors the API response structure (tabs have nested documentTab + tabProperties).
type RawTab struct {
	TabProperties TabProperties `json:"tabProperties"`
	DocumentTab   DocumentTab   `json:"documentTab"`
	ChildTabs     []RawTab      `json:"childTabs"`
}

// TabProperties holds identifying information for a document tab.
type TabProperties struct {
	TabID string `json:"tabId"`
	Title string `json:"title"`
}

// DocumentTab holds the content of a document tab.
type DocumentTab struct {
	Body  Body            `json:"body"`
	Lists json.RawMessage `json:"lists"`
}

// Body holds the structural content blocks of a document.
type Body struct {
	Content []Block `json:"content"`
}

// Block is a structural element: paragraph, table, or section break.
type Block struct {
	Paragraph    *Paragraph    `json:"paragraph,omitempty"`
	Table        *Table        `json:"table,omitempty"`
	SectionBreak *SectionBreak `json:"sectionBreak,omitempty"`
}

// Paragraph holds inline elements and style information.
type Paragraph struct {
	Elements       []Element      `json:"elements"`
	ParagraphStyle ParagraphStyle `json:"paragraphStyle"`
	Bullet         *Bullet        `json:"bullet,omitempty"`
}

// Element is an inline content element within a paragraph.
type Element struct {
	TextRun             *TextRun             `json:"textRun,omitempty"`
	InlineObjectElement *InlineObjectElement `json:"inlineObjectElement,omitempty"`
}

// TextRun is a contiguous run of text with uniform styling.
type TextRun struct {
	Content   string    `json:"content"`
	TextStyle TextStyle `json:"textStyle"`
}

// TextStyle holds formatting attributes for a text run.
type TextStyle struct {
	Bold          bool  `json:"bold"`
	Italic        bool  `json:"italic"`
	Strikethrough bool  `json:"strikethrough"`
	Link          *Link `json:"link,omitempty"`
}

// Link holds a hyperlink URL.
type Link struct {
	URL string `json:"url"`
}

// InlineObjectElement references an embedded object (e.g. image).
type InlineObjectElement struct {
	InlineObjectID string `json:"inlineObjectId"`
}

// ParagraphStyle holds paragraph-level formatting.
type ParagraphStyle struct {
	NamedStyleType string `json:"namedStyleType"`
	HeadingID      string `json:"headingId"`
}

// Bullet describes list membership for a paragraph.
type Bullet struct {
	ListID       string    `json:"listId"`
	NestingLevel int       `json:"nestingLevel"`
	TextStyle    TextStyle `json:"textStyle"`
}

// Table represents a table structural element.
type Table struct {
	Rows      int        `json:"rows"`
	Columns   int        `json:"columns"`
	TableRows []TableRow `json:"tableRows"`
}

// TableRow holds the cells of a single table row.
type TableRow struct {
	TableCells []TableCell `json:"tableCells"`
}

// TableCell holds the content blocks within a table cell.
type TableCell struct {
	Content []Block `json:"content"`
}

// SectionBreak represents a section break in the document.
type SectionBreak struct{}

// List describes a list definition referenced by bullet paragraphs.
type List struct {
	ListProperties ListProperties `json:"listProperties"`
}

// ListProperties holds the nesting-level configuration for a list.
type ListProperties struct {
	NestingLevels []NestingLevel `json:"nestingLevels"`
}

// NestingLevel describes the glyph style for one level of a list.
type NestingLevel struct {
	GlyphType   string `json:"glyphType"`
	GlyphFormat string `json:"glyphFormat"`
	StartNumber int    `json:"startNumber"`
}

// AllTabs flattens the tab tree (including child tabs) into an ordered slice.
func (d *Document) AllTabs() []Tab {
	var tabs []Tab
	for _, raw := range d.Tabs {
		tabs = append(tabs, flattenTab(raw)...)
	}
	return tabs
}

func flattenTab(raw RawTab) []Tab {
	lists := parseLists(raw.DocumentTab.Lists)
	tab := Tab{
		Title: raw.TabProperties.Title,
		TabID: raw.TabProperties.TabID,
		Body:  raw.DocumentTab.Body,
		Lists: lists,
	}
	result := []Tab{tab}
	for _, child := range raw.ChildTabs {
		result = append(result, flattenTab(child)...)
	}
	return result
}

func parseLists(raw json.RawMessage) map[string]List {
	if raw == nil {
		return nil
	}
	var lists map[string]List
	if err := json.Unmarshal(raw, &lists); err != nil {
		return nil
	}
	return lists
}
