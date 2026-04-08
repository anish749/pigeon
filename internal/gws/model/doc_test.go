package model

import (
	"encoding/json"
	"testing"
)

func TestFlattenTab_Single(t *testing.T) {
	raw := RawTab{
		TabProperties: TabProperties{TabID: "t1", Title: "Main"},
		DocumentTab: DocumentTab{
			Body: Body{Content: []Block{{Paragraph: &Paragraph{
				Elements: []Element{{TextRun: &TextRun{Content: "hello"}}},
			}}}},
		},
	}
	tabs, err := flattenTab(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 {
		t.Fatalf("got %d tabs, want 1", len(tabs))
	}
	if tabs[0].Title != "Main" || tabs[0].TabID != "t1" {
		t.Errorf("tab = {Title:%q, TabID:%q}, want {Main, t1}", tabs[0].Title, tabs[0].TabID)
	}
}

func TestFlattenTab_WithChildren(t *testing.T) {
	raw := RawTab{
		TabProperties: TabProperties{TabID: "parent", Title: "Parent"},
		DocumentTab:   DocumentTab{Body: Body{}},
		ChildTabs: []RawTab{
			{
				TabProperties: TabProperties{TabID: "child1", Title: "Child 1"},
				DocumentTab:   DocumentTab{Body: Body{}},
			},
			{
				TabProperties: TabProperties{TabID: "child2", Title: "Child 2"},
				DocumentTab:   DocumentTab{Body: Body{}},
				ChildTabs: []RawTab{
					{
						TabProperties: TabProperties{TabID: "grandchild", Title: "Grandchild"},
						DocumentTab:   DocumentTab{Body: Body{}},
					},
				},
			},
		},
	}
	tabs, err := flattenTab(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Parent", "Child 1", "Child 2", "Grandchild"}
	if len(tabs) != len(want) {
		t.Fatalf("got %d tabs, want %d", len(tabs), len(want))
	}
	for i, w := range want {
		if tabs[i].Title != w {
			t.Errorf("tabs[%d].Title = %q, want %q", i, tabs[i].Title, w)
		}
	}
}

func TestParseInlineObjects(t *testing.T) {
	raw := map[string]json.RawMessage{
		"obj1": json.RawMessage(`{
			"inlineObjectProperties": {
				"embeddedObject": {
					"title": "Logo",
					"imageProperties": {
						"contentUri": "https://example.com/logo.png"
					}
				}
			}
		}`),
	}
	got, err := parseInlineObjects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1", len(got))
	}
	obj := got["obj1"]
	if obj.ObjectID != "obj1" {
		t.Errorf("ObjectID = %q, want %q", obj.ObjectID, "obj1")
	}
	if obj.ImageURI != "https://example.com/logo.png" {
		t.Errorf("ImageURI = %q, want %q", obj.ImageURI, "https://example.com/logo.png")
	}
	if obj.Title != "Logo" {
		t.Errorf("Title = %q, want %q", obj.Title, "Logo")
	}
}

func TestParseInlineObjects_Empty(t *testing.T) {
	got, err := parseInlineObjects(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestParseLists(t *testing.T) {
	raw := json.RawMessage(`{
		"list1": {
			"listProperties": {
				"nestingLevels": [
					{"glyphType": "DECIMAL", "glyphFormat": "%0.", "startNumber": 1}
				]
			}
		}
	}`)
	got, err := parseLists(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lists, want 1", len(got))
	}
	list := got["list1"]
	if len(list.ListProperties.NestingLevels) != 1 {
		t.Fatalf("got %d nesting levels, want 1", len(list.ListProperties.NestingLevels))
	}
	nl := list.ListProperties.NestingLevels[0]
	if nl.GlyphType != "DECIMAL" {
		t.Errorf("GlyphType = %q, want DECIMAL", nl.GlyphType)
	}
	if nl.StartNumber != 1 {
		t.Errorf("StartNumber = %d, want 1", nl.StartNumber)
	}
}

func TestParseLists_Nil(t *testing.T) {
	got, err := parseLists(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}
