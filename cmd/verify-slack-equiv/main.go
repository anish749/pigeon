// Command verify-slack-equiv walks every Slack JSONL under
// ~/.local/share/pigeon/slack and measures what transformation Slack applies
// when it produces the message `text` field from the `blocks` field. It does
// so by hypothesis testing: render each rich_text block under a set of
// candidate rules (e.g. "bold spans wrap as *text*", "& becomes &amp;"),
// compare against the stored text (post-resolve), and count how often each
// rule actually holds on real data.
//
// The goal is to derive the renderer's rules from the data rather than
// guessing defensively.
//
// Stored `text` is post-resolve (user/channel mentions rewritten to names).
// Blocks carry the wire form (<@Uxxx>, <#Cxxx>). Matching therefore splits
// the render into literal segments separated by mention placeholders, then
// walks the stored text forward matching each literal; whatever lies between
// is accepted as the resolved mention.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

type rules struct {
	Bold       bool
	Italic     bool
	Strike     bool
	HTMLEscape bool
}

// all enabled, for the maximal-rule render attempt.
var rulesAll = rules{Bold: true, Italic: true, Strike: true, HTMLEscape: true}

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
	MultiSection   bool // more than one rich_text_section in the block
	Unsupported    bool // a child element kind we don't render (list, quote, preformatted, etc.)
	NotRichText    bool // top-level block isn't rich_text
	MultipleBlocks bool
}

const mentionPlaceholder = "\x00M\x00"

type outcome struct {
	Matched    bool   // did any rule combination reach equivalence?
	UsedRules  rules  // which rules were required (minimal set that matched)
	WireRender string // render with no style rules, for divergent samples
	Stored     string
}

func main() {
	root := filepath.Join(os.Getenv("HOME"), ".local", "share", "pigeon", "slack")

	c := counters{
		ruleUsed:          map[string]int{},
		ruleApplicable:    map[string]int{},
		ruleMatchesGiven:  map[string]int{},
		ruleSoleFix:       map[string]int{},
		featureMatchGiven: map[string]int{},
		featureAny:        map[string]int{},
	}

	var divergentSamples []outcome

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
			if err != nil {
				continue
			}
			if line.Msg == nil || line.Msg.RawType != modelv1.RawTypeSlack {
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

			rt := blocks.BlockSet[0].(*goslack.RichTextBlock)
			stored := line.Msg.Text

			// Feature counts for observational reporting.
			if feat.HasMention {
				c.featureAny["mention"]++
			}
			if feat.HasEmoji {
				c.featureAny["emoji"]++
			}
			if feat.HasLink {
				c.featureAny["link"]++
			}
			if feat.HasCode {
				c.featureAny["code"]++
			}

			// Rule applicability counts (for verification rate).
			if feat.HasBold {
				c.ruleApplicable["bold"]++
			}
			if feat.HasItalic {
				c.ruleApplicable["italic"]++
			}
			if feat.HasStrike {
				c.ruleApplicable["strike"]++
			}
			if feat.HasEscapable {
				c.ruleApplicable["html_escape"]++
			}

			// Strict attempt: no rules applied at all (not even html escape).
			// Must reach stored text via direct or mention-resolved match.
			if s, ok := render(rt, rules{}); ok && matchOrMentionMatch(s, stored) {
				c.directNoRules++
				// Subclassify into direct vs mention for parity with older numbers.
				if s == stored {
					// pure direct
				} else {
					c.mentionNoRules++
				}
				continue
			}

			// Try maximal rules.
			maxRender, ok := render(rt, rulesAll)
			maxMatches := ok && matchOrMentionMatch(maxRender, stored)

			// Test each rule in isolation to measure per-rule contribution.
			// "sole_fix" means enabling *only* this rule flipped the strict
			// failure into a match — the rule alone is necessary and sufficient
			// for this message. Lets us verify rules that aren't entangled.
			for _, r := range []string{"bold", "italic", "strike", "html_escape"} {
				trial := rules{}
				switch r {
				case "bold":
					if !feat.HasBold {
						continue
					}
					trial.Bold = true
				case "italic":
					if !feat.HasItalic {
						continue
					}
					trial.Italic = true
				case "strike":
					if !feat.HasStrike {
						continue
					}
					trial.Strike = true
				case "html_escape":
					if !feat.HasEscapable {
						continue
					}
					trial.HTMLEscape = true
				}
				out, ok := render(rt, trial)
				if ok && matchOrMentionMatch(out, stored) {
					c.ruleSoleFix[r]++
				}
			}

			if !maxMatches {
				c.divergent++
				// Show the full-rule render so styled-span samples display
				// meaningfully. The render still has mention placeholders
				// (shown as "\x00M\x00"); readers can infer what resolves.
				if len(divergentSamples) < 20 {
					divergentSamples = append(divergentSamples, outcome{
						WireRender: maxRender,
						Stored:     stored,
					})
				}
				continue
			}

			// Maximal match succeeded. Find minimal rule subset that matches.
			used := minimalRuleSet(rt, stored, feat)
			c.matchedWithRules++
			if used.Bold {
				c.ruleUsed["bold"]++
			}
			if used.Italic {
				c.ruleUsed["italic"]++
			}
			if used.Strike {
				c.ruleUsed["strike"]++
			}
			if used.HTMLEscape {
				c.ruleUsed["html_escape"]++
			}

			// Rule-matches-given: feature present and (with rules allowed) it resolved.
			if feat.HasBold {
				c.ruleMatchesGiven["bold"]++
			}
			if feat.HasItalic {
				c.ruleMatchesGiven["italic"]++
			}
			if feat.HasStrike {
				c.ruleMatchesGiven["strike"]++
			}
			if feat.HasEscapable {
				c.ruleMatchesGiven["html_escape"]++
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(1)
	}

	report(c, divergentSamples)
}

// analyzeFeatures inspects a single block and records what kinds of content
// it contains. Returns (_, false) if the block isn't rich_text.
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
	if len(rt.Elements) > 1 {
		feat.MultiSection = true
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

func containsEscapable(s string) bool {
	return strings.ContainsAny(s, "&<>")
}

// render converts a rich_text block to its stored-text form under the given
// rules. Non-text element kinds (mentions) are rendered as a placeholder so
// the harness can match against post-resolve text. Fails (returns false) if
// any style flag is set on an element but the corresponding rule is off — so
// the strict call (rules{}) fails whenever styling is present.
func render(rt *goslack.RichTextBlock, r rules) (string, bool) {
	var b strings.Builder
	for i, el := range rt.Elements {
		if i > 0 {
			b.WriteByte('\n')
		}
		sec, ok := el.(*goslack.RichTextSection)
		if !ok {
			return "", false
		}
		if !renderSection(&b, sec, r) {
			return "", false
		}
	}
	return b.String(), true
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func renderSection(b *strings.Builder, s *goslack.RichTextSection, r rules) bool {
	var bold, italic, strike, code bool
	for _, el := range s.Elements {
		st := styleOf(el)
		// Respect the rules mask: a style that's present but not enabled in
		// the rule set causes the render to fail, mirroring the strict
		// pre-rule behavior used for ablation.
		if st != nil {
			if st.Bold && !r.Bold {
				return false
			}
			if st.Italic && !r.Italic {
				return false
			}
			if st.Strike && !r.Strike {
				return false
			}
		}
		// Close styles being turned off, innermost first.
		if code && (st == nil || !st.Code) {
			b.WriteByte('`')
			code = false
		}
		if strike && (st == nil || !st.Strike) {
			b.WriteByte('~')
			strike = false
		}
		if italic && (st == nil || !st.Italic) {
			b.WriteByte('_')
			italic = false
		}
		if bold && (st == nil || !st.Bold) {
			b.WriteByte('*')
			bold = false
		}
		// Open styles being turned on, outermost first.
		if st != nil {
			if st.Bold && !bold {
				b.WriteByte('*')
				bold = true
			}
			if st.Italic && !italic {
				b.WriteByte('_')
				italic = true
			}
			if st.Strike && !strike {
				b.WriteByte('~')
				strike = true
			}
			if st.Code && !code {
				b.WriteByte('`')
				code = true
			}
		}
		if !renderContent(b, el, r) {
			return false
		}
	}
	if code {
		b.WriteByte('`')
	}
	if strike {
		b.WriteByte('~')
	}
	if italic {
		b.WriteByte('_')
	}
	if bold {
		b.WriteByte('*')
	}
	return true
}

func styleOf(el goslack.RichTextSectionElement) *goslack.RichTextSectionTextStyle {
	switch e := el.(type) {
	case *goslack.RichTextSectionTextElement:
		return e.Style
	case *goslack.RichTextSectionUserElement:
		return e.Style
	case *goslack.RichTextSectionChannelElement:
		return e.Style
	case *goslack.RichTextSectionEmojiElement:
		return e.Style
	case *goslack.RichTextSectionLinkElement:
		return e.Style
	case *goslack.RichTextSectionTeamElement:
		return e.Style
	}
	return nil
}

func renderContent(b *strings.Builder, el goslack.RichTextSectionElement, r rules) bool {
	maybeEscape := func(s string) string {
		if r.HTMLEscape {
			return htmlEscaper.Replace(s)
		}
		return s
	}
	switch e := el.(type) {
	case *goslack.RichTextSectionTextElement:
		b.WriteString(maybeEscape(e.Text))
	case *goslack.RichTextSectionUserElement,
		*goslack.RichTextSectionChannelElement,
		*goslack.RichTextSectionUserGroupElement,
		*goslack.RichTextSectionBroadcastElement:
		b.WriteString(mentionPlaceholder)
	case *goslack.RichTextSectionLinkElement:
		if e.Text == "" {
			fmt.Fprintf(b, "<%s>", maybeEscape(e.URL))
		} else {
			fmt.Fprintf(b, "<%s|%s>", maybeEscape(e.URL), maybeEscape(e.Text))
		}
	case *goslack.RichTextSectionEmojiElement:
		fmt.Fprintf(b, ":%s:", e.Name)
	default:
		return false
	}
	return true
}

// matchOrMentionMatch compares a rendered string (with mention placeholders)
// against stored text, allowing each placeholder to stand for any non-empty
// resolved value.
func matchOrMentionMatch(rendered, stored string) bool {
	if !strings.Contains(rendered, mentionPlaceholder) {
		return rendered == stored
	}
	return matchesWithMentions(stored, rendered)
}

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
	return len(stored) >= 1 || len(parts) == 1
}

// minimalRuleSet finds the smallest rule subset whose rendered output matches
// storedText. We already know rulesAll matches. We try each ablation by
// turning off one rule at a time; if the match still holds, that rule wasn't
// needed. Rules not relevant to the block (no bold span → bold off) don't
// change the render, so this doesn't inflate attribution.
func minimalRuleSet(rt *goslack.RichTextBlock, stored string, feat features) rules {
	cand := rulesAll
	toggle := []func(r *rules){
		func(r *rules) { r.Bold = false },
		func(r *rules) { r.Italic = false },
		func(r *rules) { r.Strike = false },
		func(r *rules) { r.HTMLEscape = false },
	}
	for _, off := range toggle {
		trial := cand
		off(&trial)
		out, ok := render(rt, trial)
		if ok && matchOrMentionMatch(out, stored) {
			cand = trial
		}
	}
	// A rule is only "used" if it was on in the minimal set AND the feature
	// was present in the block.
	if !feat.HasBold {
		cand.Bold = false
	}
	if !feat.HasItalic {
		cand.Italic = false
	}
	if !feat.HasStrike {
		cand.Strike = false
	}
	if !feat.HasEscapable {
		cand.HTMLEscape = false
	}
	return cand
}

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

type counters struct {
	total             int
	withBlocks        int
	hasAttachOrFile   int
	directNoRules     int
	mentionNoRules    int
	matchedWithRules  int
	ruleUsed          map[string]int
	ruleApplicable    map[string]int
	ruleMatchesGiven  map[string]int
	ruleSoleFix       map[string]int
	featureMatchGiven map[string]int
	featureAny        map[string]int
	divergent         int
	multiBlock        int
	notRichText       int
	unsupported       int
}

func report(c counters, samples []outcome) {
	fmt.Printf("Slack messages scanned:           %d\n", c.total)
	fmt.Printf("With blocks, no attachments:      %d\n", c.withBlocks)
	fmt.Printf("  not rich_text (kept):           %d\n", c.notRichText)
	fmt.Printf("  multi-block (kept):             %d\n", c.multiBlock)
	fmt.Printf("  unsupported element (kept):     %d\n", c.unsupported)
	matchable := c.withBlocks - c.notRichText - c.multiBlock - c.unsupported
	fmt.Printf("  matchable single rich_text:     %d\n\n", matchable)

	strict := c.directNoRules
	withRules := c.matchedWithRules
	matched := strict + withRules
	fmt.Println("Matchable breakdown:")
	fmt.Printf("  matched strict (no style/entity rules): %d  (%.2f%%)\n",
		strict, pct(strict, matchable))
	fmt.Printf("  matched only after rules applied:       %d  (%.2f%%)\n",
		withRules, pct(withRules, matchable))
	fmt.Printf("  still divergent after all rules:        %d  (%.2f%%)\n\n",
		c.divergent, pct(c.divergent, matchable))
	fmt.Printf("  total resolved under some rule set:     %d  (%.2f%%)\n\n",
		matched, pct(matched, matchable))

	// Rule verification: for each rule, how often was it needed, and of
	// messages where the rule's feature was present, what fraction resolved
	// (under full rule set)?
	// Per-rule verification:
	//   feat-count    — messages where the rule's feature is present in the block
	//   sole-fix      — messages where enabling ONLY this rule flips strict-fail to match
	//                   (strong evidence the rule faithfully describes Slack's behavior)
	//   needed-by     — messages in the minimal-rule match where this rule was required
	//   any-resolve   — messages with the feature that resolve under full rules
	fmt.Println("Per-rule verification:")
	fmt.Printf("  %-14s %-11s %-11s %-11s %s\n", "rule", "feat-count", "sole-fix", "needed-by", "any-resolve")
	for _, r := range []string{"bold", "italic", "strike", "html_escape"} {
		feat := c.ruleApplicable[r]
		sole := c.ruleSoleFix[r]
		need := c.ruleUsed[r]
		matched := c.ruleMatchesGiven[r]
		fmt.Printf("  %-14s %-11d %-11d %-11d %d / %d (%.2f%%)\n",
			r, feat, sole, need, matched, feat, pct(matched, feat))
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
			fmt.Printf("    render: %s\n", truncate(s.WireRender, 220))
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
