package converter

import (
	"testing"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestHeadingParagraph(t *testing.T) {
	tab := modelv1.Tab{
		Body: modelv1.Body{
			Content: []modelv1.Block{
				{Paragraph: &modelv1.Paragraph{
					Elements: []modelv1.Element{
						{TextRun: &modelv1.TextRun{Content: "Title\n"}},
					},
					ParagraphStyle: modelv1.ParagraphStyle{NamedStyleType: "HEADING_1"},
				}},
			},
		},
	}

	c := NewMarkdownConverter()
	got := c.Convert(tab).Markdown
	want := "# Title\n\n"
	if got != want {
		t.Errorf("heading: got %q, want %q", got, want)
	}
}

func TestBoldItalicTextRun(t *testing.T) {
	tab := modelv1.Tab{
		Body: modelv1.Body{
			Content: []modelv1.Block{
				{Paragraph: &modelv1.Paragraph{
					Elements: []modelv1.Element{
						{TextRun: &modelv1.TextRun{
							Content:   "text",
							TextStyle: modelv1.TextStyle{Bold: true, Italic: true},
						}},
					},
					ParagraphStyle: modelv1.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
				}},
			},
		},
	}

	c := NewMarkdownConverter()
	got := c.Convert(tab).Markdown
	want := "_**text**_\n\n"
	if got != want {
		t.Errorf("bold+italic: got %q, want %q", got, want)
	}
}

func TestBulletList(t *testing.T) {
	tab := modelv1.Tab{
		Body: modelv1.Body{
			Content: []modelv1.Block{
				{Paragraph: &modelv1.Paragraph{
					Elements: []modelv1.Element{
						{TextRun: &modelv1.TextRun{Content: "item"}},
					},
					Bullet: &modelv1.Bullet{ListID: "list1", NestingLevel: 0},
				}},
			},
		},
	}

	c := NewMarkdownConverter()
	got := c.Convert(tab).Markdown
	want := "- item\n"
	if got != want {
		t.Errorf("bullet: got %q, want %q", got, want)
	}
}

func TestTable(t *testing.T) {
	tab := modelv1.Tab{
		Body: modelv1.Body{
			Content: []modelv1.Block{
				{Table: &modelv1.Table{
					Rows:    2,
					Columns: 2,
					TableRows: []modelv1.TableRow{
						{TableCells: []modelv1.TableCell{
							{Content: []modelv1.Block{{Paragraph: &modelv1.Paragraph{
								Elements: []modelv1.Element{{TextRun: &modelv1.TextRun{Content: "A"}}},
							}}}},
							{Content: []modelv1.Block{{Paragraph: &modelv1.Paragraph{
								Elements: []modelv1.Element{{TextRun: &modelv1.TextRun{Content: "B"}}},
							}}}},
						}},
						{TableCells: []modelv1.TableCell{
							{Content: []modelv1.Block{{Paragraph: &modelv1.Paragraph{
								Elements: []modelv1.Element{{TextRun: &modelv1.TextRun{Content: "C"}}},
							}}}},
							{Content: []modelv1.Block{{Paragraph: &modelv1.Paragraph{
								Elements: []modelv1.Element{{TextRun: &modelv1.TextRun{Content: "D"}}},
							}}}},
						}},
					},
				}},
			},
		},
	}

	c := NewMarkdownConverter()
	got := c.Convert(tab).Markdown
	want := "| A | B |\n| --- | --- |\n| C | D |\n\n"
	if got != want {
		t.Errorf("table: got %q, want %q", got, want)
	}
}

func TestLink(t *testing.T) {
	tab := modelv1.Tab{
		Body: modelv1.Body{
			Content: []modelv1.Block{
				{Paragraph: &modelv1.Paragraph{
					Elements: []modelv1.Element{
						{TextRun: &modelv1.TextRun{
							Content:   "click here",
							TextStyle: modelv1.TextStyle{Link: &modelv1.Link{URL: "https://example.com"}},
						}},
					},
					ParagraphStyle: modelv1.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
				}},
			},
		},
	}

	c := NewMarkdownConverter()
	got := c.Convert(tab).Markdown
	want := "[click here](https://example.com)\n\n"
	if got != want {
		t.Errorf("link: got %q, want %q", got, want)
	}
}
