package converter

import (
	"testing"

	"github.com/anish749/pigeon/internal/gws/model"
)

func TestHeadingParagraph(t *testing.T) {
	tab := model.Tab{
		Body: model.Body{
			Content: []model.Block{
				{Paragraph: &model.Paragraph{
					Elements: []model.Element{
						{TextRun: &model.TextRun{Content: "Title\n"}},
					},
					ParagraphStyle: model.ParagraphStyle{NamedStyleType: "HEADING_1"},
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
	tab := model.Tab{
		Body: model.Body{
			Content: []model.Block{
				{Paragraph: &model.Paragraph{
					Elements: []model.Element{
						{TextRun: &model.TextRun{
							Content:   "text",
							TextStyle: model.TextStyle{Bold: true, Italic: true},
						}},
					},
					ParagraphStyle: model.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
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
	tab := model.Tab{
		Body: model.Body{
			Content: []model.Block{
				{Paragraph: &model.Paragraph{
					Elements: []model.Element{
						{TextRun: &model.TextRun{Content: "item"}},
					},
					Bullet: &model.Bullet{ListID: "list1", NestingLevel: 0},
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
	tab := model.Tab{
		Body: model.Body{
			Content: []model.Block{
				{Table: &model.Table{
					Rows:    2,
					Columns: 2,
					TableRows: []model.TableRow{
						{TableCells: []model.TableCell{
							{Content: []model.Block{{Paragraph: &model.Paragraph{
								Elements: []model.Element{{TextRun: &model.TextRun{Content: "A"}}},
							}}}},
							{Content: []model.Block{{Paragraph: &model.Paragraph{
								Elements: []model.Element{{TextRun: &model.TextRun{Content: "B"}}},
							}}}},
						}},
						{TableCells: []model.TableCell{
							{Content: []model.Block{{Paragraph: &model.Paragraph{
								Elements: []model.Element{{TextRun: &model.TextRun{Content: "C"}}},
							}}}},
							{Content: []model.Block{{Paragraph: &model.Paragraph{
								Elements: []model.Element{{TextRun: &model.TextRun{Content: "D"}}},
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
	tab := model.Tab{
		Body: model.Body{
			Content: []model.Block{
				{Paragraph: &model.Paragraph{
					Elements: []model.Element{
						{TextRun: &model.TextRun{
							Content:   "click here",
							TextStyle: model.TextStyle{Link: &model.Link{URL: "https://example.com"}},
						}},
					},
					ParagraphStyle: model.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
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
