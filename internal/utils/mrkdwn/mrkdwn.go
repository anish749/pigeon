// Package mrkdwn converts standard Markdown to Slack mrkdwn format.
//
// It uses goldmark to parse Markdown into an AST, then renders each node
// into the equivalent Slack mrkdwn syntax. This avoids the ordering bugs
// that plague regex-based converters (e.g. bold vs bullet collisions).
package mrkdwn

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// ToSlackMarkdown converts standard Markdown text to Slack mrkdwn.
func ToSlackMarkdown(s string) string {
	source := []byte(s)
	md := goldmark.New(
		// Add the strikethrough parser only (not the HTML renderer that comes
		// with extension.Strikethrough — we provide our own mrkdwn renderer).
		goldmark.WithParserOptions(parser.WithInlineParsers(
			util.Prioritized(extension.NewStrikethroughParser(), 500),
		)),
		goldmark.WithRenderer(renderer.NewRenderer(
			renderer.WithNodeRenderers(util.Prioritized(&nodeRenderer{}, 1000)),
		)),
	)
	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		return s // fall back to original on error
	}
	return strings.TrimSpace(buf.String())
}

type nodeRenderer struct{}

func (r *nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// blocks
	reg.Register(ast.KindDocument, r.renderDocument)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindThematicBreak, r.renderThematicBreak)
	reg.Register(ast.KindTextBlock, r.renderTextBlock)
	reg.Register(ast.KindLinkReferenceDefinition, r.skip)

	// inlines
	reg.Register(ast.KindText, r.renderText)
	reg.Register(ast.KindString, r.renderString)
	reg.Register(ast.KindEmphasis, r.renderEmphasis)
	reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindRawHTML, r.renderRawHTML)

	// GFM extensions
	reg.Register(east.KindStrikethrough, r.renderStrikethrough)
}

// blocks

func (r *nodeRenderer) renderDocument(_ util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderParagraph(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		// Add blank line between paragraphs, but not after the last one.
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderHeading(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_ = w.WriteByte('*')
	} else {
		_ = w.WriteByte('*')
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderBlockquote(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("> ")
	} else {
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderCodeBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("```\n")
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			w.Write(seg.Value(source))
		}
		_, _ = w.WriteString("```")
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("```\n")
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			w.Write(seg.Value(source))
		}
		_, _ = w.WriteString("```")
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderList(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering && n.NextSibling() != nil {
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderListItem(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		list := n.Parent().(*ast.List)
		if list.IsOrdered() {
			idx := 1
			if list.Start > 0 {
				idx = list.Start
			}
			for sib := n.PreviousSibling(); sib != nil; sib = sib.PreviousSibling() {
				idx++
			}
			_, _ = fmt.Fprintf(w, "%d. ", idx)
		} else {
			_, _ = w.WriteString("• ")
		}
	} else {
		_ = w.WriteByte('\n')
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderThematicBreak(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("---")
		if n.NextSibling() != nil {
			_, _ = w.WriteString("\n\n")
		}
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderTextBlock(w util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

// inlines

func (r *nodeRenderer) renderText(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	t := n.(*ast.Text)
	w.Write(t.Value(source))
	if t.SoftLineBreak() {
		_ = w.WriteByte('\n')
	}
	if t.HardLineBreak() {
		_ = w.WriteByte('\n')
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderString(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		s := n.(*ast.String)
		w.Write(s.Value)
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderEmphasis(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	em := n.(*ast.Emphasis)
	if em.Level == 2 {
		_ = w.WriteByte('*')
	} else {
		_ = w.WriteByte('_')
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderCodeSpan(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_ = w.WriteByte('`')
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			t := child.(*ast.Text)
			w.Write(t.Value(source))
		}
		_ = w.WriteByte('`')
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderLink(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		link := n.(*ast.Link)
		_, _ = fmt.Fprintf(w, "<%s|", link.Destination)
	} else {
		_ = w.WriteByte('>')
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderAutoLink(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		al := n.(*ast.AutoLink)
		url := al.URL(source)
		_, _ = fmt.Fprintf(w, "<%s>", url)
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderImage(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		img := n.(*ast.Image)
		_, _ = fmt.Fprintf(w, "<%s>", img.Destination)
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderRawHTML(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		rh := n.(*ast.RawHTML)
		segs := rh.Segments
		for i := range segs.Len() {
			seg := segs.At(i)
			w.Write(seg.Value(source))
		}
	}
	return ast.WalkContinue, nil
}

// GFM extensions

func (r *nodeRenderer) renderStrikethrough(w util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	_ = w.WriteByte('~')
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) skip(_ util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkSkipChildren, nil
}

