// Command verify-slack-equiv walks every Slack JSONL under
// ~/.local/share/pigeon/slack and measures how often Slack's text fallback
// is byte-for-byte what slackraw.RenderRichTextForVerify produces. The
// harness does not contain a renderer — it consumes the production one and
// ablates individual rules by either stripping the corresponding style flag
// from a cloned block (bold/italic/strike) or reverse-escaping the output
// (HTML escape).
//
// Stored `text` is post-resolve (user/channel mentions rewritten to names).
// Blocks carry the wire form (<@Uxxx>, <#Cxxx>). Matching therefore splits
// the render into literal segments separated by a mention placeholder, then
// walks the stored text forward matching each literal; whatever lies between
// is accepted as the resolved mention.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// features records which rule opportunities exist in a single block:
// whether it actually contains a bold span, an escapable character, etc.
type features struct {
	HasBold        bool
	HasItalic      bool
	HasStrike      bool
	HasEscapable   bool // any & < > in the block's rendered output
	HasMention     bool // user / channel / usergroup / broadcast
	HasEmoji       bool
	HasLink        bool
	HasCode        bool
	Unsupported    bool // a child element kind the production renderer can't handle
	NotRichText    bool
	MultipleBlocks bool
}

const mentionPlaceholder = "\x00M\x00"

// wireMentionRe matches every wire-form mention token the production
// renderer emits. We translate them to a sentinel before comparing against
// post-resolve stored text. The ID character classes are deliberately loose
// — external-workspace user IDs start with W, private-channel IDs with G,
// and subteam IDs don't follow a strict prefix rule.
var wireMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>|<#[A-Z][A-Z0-9]+>|<!subteam\^[A-Z0-9]+>|<!channel>|<!here>|<!everyone>`)

// htmlEscapeReverse undoes the production renderer's HTML escape. Used for
// ablation: render normally, reverse-escape, compare — if the reverse
// matches stored text, the escape rule wasn't contributing to the match.
var htmlEscapeReverse = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

type sample struct {
	Stored string
	Render string
}

type counters struct {
	total           int
	withBlocks      int
	hasAttachOrFile int
	matchedFull     int // matched with production renderer (full rules)
	divergent       int
	multiBlock      int
	notRichText     int
	unsupported     int

	ruleApplicable map[string]int // messages where the rule's feature is present
	ruleNeeded     map[string]int // messages where removing the rule breaks the match
	featureAny     map[string]int // observational counts (mention/emoji/link/code)
}

func main() {
	root := filepath.Join(os.Getenv("HOME"), ".local", "share", "pigeon", "slack")

	c := counters{
		ruleApplicable: map[string]int{},
		ruleNeeded:     map[string]int{},
		featureAny:     map[string]int{},
	}
	var divergentSamples []sample

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			line, err := modelv1.Parse(sc.Text())
			if err != nil || line.Msg == nil || line.Msg.RawType != modelv1.RawTypeSlack {
				continue
			}
			c.total++
			raw := line.Msg.Raw
			if raw == nil {
				continue
			}
			if _, ok := raw["attachments"]; ok {
				c.hasAttachOrFile++
				continue
			}
			if _, ok := raw["files"]; ok {
				c.hasAttachOrFile++
				continue
			}
			blocksAny, ok := raw["blocks"]
			if !ok {
				continue
			}
			blocks, ok := parseBlocks(blocksAny)
			if !ok || len(blocks.BlockSet) == 0 {
				continue
			}
			c.withBlocks++
			feat, ok := analyzeFeatures(blocks)
			if !ok {
				c.notRichText++
				continue
			}
			if feat.MultipleBlocks {
				c.multiBlock++
				continue
			}
			if feat.Unsupported {
				c.unsupported++
				continue
			}
			process(blocks, line.Msg.Text, feat, &c, &divergentSamples)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(1)
	}
	report(c, divergentSamples)
}

// process runs the production renderer on one message and records counters.
// All rule ablation is done by either cloning the block with a style flag
// cleared (bold/italic/strike) or reverse-escaping the output (html_escape).
// Nothing in this function duplicates the rendering logic that lives in
// internal/store/modelv1/slackraw.
func process(blocks goslack.Blocks, stored string, feat features, c *counters, samples *[]sample) {
	rt := blocks.BlockSet[0].(*goslack.RichTextBlock)

	// Feature counts (observational).
	for k, have := range map[string]bool{
		"mention": feat.HasMention,
		"emoji":   feat.HasEmoji,
		"link":    feat.HasLink,
		"code":    feat.HasCode,
	} {
		if have {
			c.featureAny[k]++
		}
	}
	for k, have := range map[string]bool{
		"bold":        feat.HasBold,
		"italic":      feat.HasItalic,
		"strike":      feat.HasStrike,
		"html_escape": feat.HasEscapable,
	} {
		if have {
			c.ruleApplicable[k]++
		}
	}

	render, ok := slackraw.RenderRichTextForVerify(rt)
	if !ok {
		// Shouldn't happen for blocks that passed the features filter.
		c.divergent++
		return
	}
	if !matchAgainstStored(render, stored) {
		c.divergent++
		if len(*samples) < 20 {
			*samples = append(*samples, sample{Stored: stored, Render: render})
		}
		return
	}
	c.matchedFull++

	// Per-rule necessity: ablate each rule and see if the match still holds.
	// If the match breaks, the rule was necessary for this message.
	if feat.HasBold && ablatedBreaks(blocks, stored, ablateBold) {
		c.ruleNeeded["bold"]++
	}
	if feat.HasItalic && ablatedBreaks(blocks, stored, ablateItalic) {
		c.ruleNeeded["italic"]++
	}
	if feat.HasStrike && ablatedBreaks(blocks, stored, ablateStrike) {
		c.ruleNeeded["strike"]++
	}
	if feat.HasEscapable {
		// HTML escape is done at render time and can't be disabled by mutating
		// the block. Simulate ablation by reverse-escaping the output.
		unescaped := htmlEscapeReverse.Replace(render)
		if !matchAgainstStored(unescaped, stored) {
			c.ruleNeeded["html_escape"]++
		}
	}
}

// ablatedBreaks clones the block, applies a style-stripping mutation, runs
// the production renderer, and reports whether the match against stored text
// is lost. If lost, the stripped rule was necessary.
func ablatedBreaks(original goslack.Blocks, stored string, ablate func(*goslack.RichTextBlock)) bool {
	clone := cloneBlocks(original)
	if len(clone.BlockSet) == 0 {
		return false
	}
	rt, ok := clone.BlockSet[0].(*goslack.RichTextBlock)
	if !ok {
		return false
	}
	ablate(rt)
	out, ok := slackraw.RenderRichTextForVerify(rt)
	if !ok {
		return true
	}
	return !matchAgainstStored(out, stored)
}

// cloneBlocks produces a deep copy by JSON round-trip. This is the cheapest
// correct way to clone goslack.Blocks because the type registry for
// polymorphic elements is handled entirely by custom UnmarshalJSON.
func cloneBlocks(bs goslack.Blocks) goslack.Blocks {
	data, err := json.Marshal(bs)
	if err != nil {
		return goslack.Blocks{}
	}
	var cp goslack.Blocks
	if err := json.Unmarshal(data, &cp); err != nil {
		return goslack.Blocks{}
	}
	return cp
}

func ablateBold(rt *goslack.RichTextBlock)   { mutateStyles(rt, func(s *goslack.RichTextSectionTextStyle) { s.Bold = false }) }
func ablateItalic(rt *goslack.RichTextBlock) { mutateStyles(rt, func(s *goslack.RichTextSectionTextStyle) { s.Italic = false }) }
func ablateStrike(rt *goslack.RichTextBlock) { mutateStyles(rt, func(s *goslack.RichTextSectionTextStyle) { s.Strike = false }) }

func mutateStyles(rt *goslack.RichTextBlock, fn func(*goslack.RichTextSectionTextStyle)) {
	for _, el := range rt.Elements {
		sec, ok := el.(*goslack.RichTextSection)
		if !ok {
			continue
		}
		for _, inner := range sec.Elements {
			var sp **goslack.RichTextSectionTextStyle
			switch e := inner.(type) {
			case *goslack.RichTextSectionTextElement:
				sp = &e.Style
			case *goslack.RichTextSectionUserElement:
				sp = &e.Style
			case *goslack.RichTextSectionChannelElement:
				sp = &e.Style
			case *goslack.RichTextSectionEmojiElement:
				sp = &e.Style
			case *goslack.RichTextSectionLinkElement:
				sp = &e.Style
			case *goslack.RichTextSectionTeamElement:
				sp = &e.Style
			default:
				continue
			}
			if *sp == nil {
				continue
			}
			fn(*sp)
		}
	}
}

// matchAgainstStored compares rendered output (wire form) to stored text
// (post-resolve). Wire-form mentions are replaced with a placeholder so that
// the stored text's resolved mention names can stand in for them.
func matchAgainstStored(rendered, stored string) bool {
	masked := wireMentionRe.ReplaceAllString(rendered, mentionPlaceholder)
	if !strings.Contains(masked, mentionPlaceholder) {
		return masked == stored
	}
	return matchesWithMentions(stored, masked)
}

// matchesWithMentions reports whether storedText can be produced from the
// placeholder string by substituting each MENTION placeholder with some
// non-empty resolved value.
func matchesWithMentions(stored, placeholder string) bool {
	parts := strings.Split(placeholder, mentionPlaceholder)
	if !strings.HasPrefix(stored, parts[0]) {
		return false
	}
	stored = stored[len(parts[0]):]
	last := parts[len(parts)-1]
	if !strings.HasSuffix(stored, last) {
		return false
	}
	stored = stored[:len(stored)-len(last)]
	middle := parts[1 : len(parts)-1]
	for _, lit := range middle {
		idx := strings.Index(stored, lit)
		if idx < 1 {
			return false
		}
		stored = stored[idx+len(lit):]
	}
	// At least one char remaining satisfies the trailing mention; if there
	// is no trailing mention (parts had length 1), stored would be empty.
	return len(parts) == 1 || len(stored) >= 1
}

func analyzeFeatures(bs goslack.Blocks) (features, bool) {
	var feat features
	if len(bs.BlockSet) > 1 {
		feat.MultipleBlocks = true
		return feat, true
	}
	rt, ok := bs.BlockSet[0].(*goslack.RichTextBlock)
	if !ok {
		feat.NotRichText = true
		return feat, false
	}
	for _, el := range rt.Elements {
		sec, isSection := el.(*goslack.RichTextSection)
		if !isSection {
			feat.Unsupported = true
			continue
		}
		for _, inner := range sec.Elements {
			switch e := inner.(type) {
			case *goslack.RichTextSectionTextElement:
				if e.Style != nil {
					if e.Style.Bold {
						feat.HasBold = true
					}
					if e.Style.Italic {
						feat.HasItalic = true
					}
					if e.Style.Strike {
						feat.HasStrike = true
					}
					if e.Style.Code {
						feat.HasCode = true
					}
				}
				if containsEscapable(e.Text) {
					feat.HasEscapable = true
				}
			case *goslack.RichTextSectionUserElement,
				*goslack.RichTextSectionChannelElement,
				*goslack.RichTextSectionUserGroupElement,
				*goslack.RichTextSectionBroadcastElement:
				feat.HasMention = true
			case *goslack.RichTextSectionLinkElement:
				feat.HasLink = true
				if containsEscapable(e.URL) || containsEscapable(e.Text) {
					feat.HasEscapable = true
				}
			case *goslack.RichTextSectionEmojiElement:
				feat.HasEmoji = true
			default:
				feat.Unsupported = true
			}
		}
	}
	return feat, true
}

func containsEscapable(s string) bool { return strings.ContainsAny(s, "&<>") }

func parseBlocks(v any) (goslack.Blocks, bool) {
	data, err := json.Marshal(v)
	if err != nil {
		return goslack.Blocks{}, false
	}
	var bs goslack.Blocks
	if err := json.Unmarshal(data, &bs); err != nil {
		return goslack.Blocks{}, false
	}
	return bs, true
}

func report(c counters, samples []sample) {
	fmt.Printf("Slack messages scanned:           %d\n", c.total)
	fmt.Printf("With blocks, no attachments:      %d\n", c.withBlocks)
	fmt.Printf("  not rich_text (kept):           %d\n", c.notRichText)
	fmt.Printf("  multi-block (kept):             %d\n", c.multiBlock)
	fmt.Printf("  unsupported element (kept):     %d\n", c.unsupported)
	matchable := c.withBlocks - c.notRichText - c.multiBlock - c.unsupported
	fmt.Printf("  matchable single rich_text:     %d\n\n", matchable)

	fmt.Println("Matchable breakdown:")
	fmt.Printf("  matched by production renderer: %d  (%.2f%%)\n",
		c.matchedFull, pct(c.matchedFull, matchable))
	fmt.Printf("  still divergent:                %d  (%.2f%%)\n\n",
		c.divergent, pct(c.divergent, matchable))

	fmt.Println("Per-rule necessity (of messages where the feature is present,")
	fmt.Println("how many matches are broken if we ablate the rule):")
	fmt.Printf("  %-14s %-12s %-12s %s\n", "rule", "feat-count", "needed-by", "necessity-rate")
	for _, r := range []string{"bold", "italic", "strike", "html_escape"} {
		feat := c.ruleApplicable[r]
		need := c.ruleNeeded[r]
		fmt.Printf("  %-14s %-12d %-12d %.2f%%\n", r, feat, need, pct(need, feat))
	}
	fmt.Println()

	fmt.Println("Feature co-occurrence (matchable population):")
	for _, k := range sortedKeys(c.featureAny) {
		fmt.Printf("  %-14s %d\n", k, c.featureAny[k])
	}
	fmt.Println()

	if len(samples) > 0 {
		fmt.Printf("=== divergent samples (%d shown of %d) ===\n", len(samples), c.divergent)
		for i, s := range samples {
			fmt.Printf("[%d] text:   %s\n", i+1, truncate(s.Stored, 220))
			fmt.Printf("    render: %s\n", truncate(s.Render, 220))
		}
	}
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

func sortedKeys(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
