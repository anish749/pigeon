// Command verify-slack-equiv walks every Slack JSONL under
// ~/.local/share/pigeon/slack and measures how often a rich_text block is
// semantically equivalent to the stored text field. The purpose is to decide
// whether it's safe to drop blocks at write time when they carry no
// information beyond text.
//
// Stored `text` is post-resolve (user/channel mentions already rewritten to
// @name / #name). Blocks carry the wire form (<@Uxxx>, <#Cxxx>). So a direct
// string compare understates true equivalence for messages that contain
// mentions. The harness reports three buckets:
//
//	direct       — rendered wire form == stored text (no mentions involved)
//	mention-only — differences are confined to mention tokens; structure matches
//	divergent    — genuine content difference (lists, styled spans, etc.)
//
// It also separates messages with attachments/files, which are kept regardless
// of whether blocks are equivalent, because those carry independent content.
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

type bucket string

const (
	bucketNoBlocks        bucket = "no_blocks"
	bucketDirectMatch     bucket = "direct_match"
	bucketMentionMatch    bucket = "mention_only_match"
	bucketUnsupported     bucket = "unsupported_block_type"
	bucketStyledSpan      bucket = "styled_span"
	bucketMultiBlock      bucket = "multi_block"
	bucketNotRichText     bucket = "not_rich_text"
	bucketDivergent        bucket = "divergent"
	bucketDivergentEntity  bucket = "divergent_html_entity"
	bucketDivergentEmoji   bucket = "divergent_emoji_alias"
	bucketAttachOrFileAlso bucket = "has_attachments_or_files"
)

type sample struct {
	path    string
	id      string
	text    string
	render  string
}

func main() {
	root := filepath.Join(os.Getenv("HOME"), ".local", "share", "pigeon", "slack")

	counts := map[bucket]int{}
	samples := map[bucket][]sample{}
	addSample := func(b bucket, s sample) {
		if len(samples[b]) < 5 {
			samples[b] = append(samples[b], s)
		}
	}

	totalMsgs := 0

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
			totalMsgs++
			raw := line.Msg.Raw
			if raw == nil {
				counts[bucketNoBlocks]++
				continue
			}
			// If attachments or files present, blocks stay regardless.
			if _, ok := raw["attachments"]; ok {
				counts[bucketAttachOrFileAlso]++
				addSample(bucketAttachOrFileAlso, sample{path: path, id: line.Msg.ID, text: line.Msg.Text})
				continue
			}
			if _, ok := raw["files"]; ok {
				counts[bucketAttachOrFileAlso]++
				continue
			}
			blocksAny, ok := raw["blocks"]
			if !ok {
				counts[bucketNoBlocks]++
				continue
			}
			blocks, ok := parseBlocks(blocksAny)
			if !ok || len(blocks.BlockSet) == 0 {
				counts[bucketNoBlocks]++
				continue
			}
			b, s := classify(blocks, line.Msg.Text)
			counts[b]++
			s.path = path
			s.id = line.Msg.ID
			addSample(b, s)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(1)
	}

	printReport(totalMsgs, counts, samples)
}

// parseBlocks takes the raw.blocks map[string]any value and reconstructs
// goslack.Blocks by round-tripping through JSON.
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

// Resolver mentions in stored text preserve display names verbatim, which can
// contain spaces (e.g. "@Donna Dev"). Regex masking is unreliable — instead
// we render the block as a sequence of literal fragments separated by mention
// sentinels, then check whether the stored text matches by walking forward
// through each literal. Whatever lies between the literals is treated as the
// resolved mention.
const mentionPlaceholder = "\x00MENTION\x00"

// classify decides which bucket a single message with blocks falls into.
func classify(bs goslack.Blocks, storedText string) (bucket, sample) {
	if len(bs.BlockSet) > 1 {
		return bucketMultiBlock, sample{text: storedText}
	}
	rt, ok := bs.BlockSet[0].(*goslack.RichTextBlock)
	if !ok {
		return bucketNotRichText, sample{text: storedText}
	}
	if b, s := richTextBucket(rt, storedText); b != "" {
		return b, s
	}
	// Render twice: once in wire form (exact fidelity) and once with mention
	// placeholders (for comparing against the post-resolve stored text).
	wire, ok := slackraw.RenderRichTextForVerify(rt)
	if !ok {
		return bucketUnsupported, sample{text: storedText}
	}
	if wire == storedText {
		return bucketDirectMatch, sample{text: storedText, render: wire}
	}
	placeholder, ok := renderWithMentionPlaceholders(rt)
	if ok && strings.Contains(placeholder, mentionPlaceholder) &&
		matchesWithMentions(storedText, placeholder) {
		return bucketMentionMatch, sample{text: storedText, render: wire}
	}
	// Subclassify divergences that look like systematic, lossy encoding.
	switch divergeReason(wire, storedText) {
	case "html_entity":
		return bucketDivergentEntity, sample{text: storedText, render: wire}
	case "emoji_alias":
		return bucketDivergentEmoji, sample{text: storedText, render: wire}
	}
	return bucketDivergent, sample{text: storedText, render: wire}
}

// divergeReason probes whether the only difference between wire and stored
// text is a known systematic rewrite Slack applies to message text fallback.
// Returns a label for the category, or "" if the divergence is something else.
func divergeReason(wire, stored string) string {
	// HTML-entity escaping: Slack rewrites & → &amp;, > → &gt;, < → &lt;
	// in the text field but not in blocks.
	entityFix := strings.NewReplacer("&amp;", "&", "&gt;", ">", "&lt;", "<")
	if entityFix.Replace(stored) == wire {
		return "html_entity"
	}
	// Emoji alias collapse: match if both sides have the same non-emoji spans
	// but different :name: tokens at the same positions.
	if onlyEmojiNamesDiffer(wire, stored) {
		return "emoji_alias"
	}
	return ""
}

// onlyEmojiNamesDiffer reports whether wire and stored are identical except
// at :name: tokens (where Slack may have swapped aliases like :+1: ↔ :thumbsup:).
func onlyEmojiNamesDiffer(wire, stored string) bool {
	re := regexp.MustCompile(`:[a-zA-Z0-9_+\-]+:`)
	wireMasked := re.ReplaceAllString(wire, "\x00E\x00")
	storedMasked := re.ReplaceAllString(stored, "\x00E\x00")
	if wireMasked != storedMasked {
		return false
	}
	// Require that at least one :name: actually appeared (otherwise we'd
	// accept trivial "no emojis at all" matches, which shouldn't happen
	// since we already compared earlier).
	return re.FindString(wire) != ""
}

// renderWithMentionPlaceholders mirrors renderRichText but substitutes a
// sentinel for user / channel / usergroup / broadcast elements, so the
// harness can match against post-resolve stored text.
func renderWithMentionPlaceholders(rt *goslack.RichTextBlock) (string, bool) {
	var b strings.Builder
	for i, el := range rt.Elements {
		if i > 0 {
			b.WriteByte('\n')
		}
		sec, ok := el.(*goslack.RichTextSection)
		if !ok {
			return "", false
		}
		for _, inner := range sec.Elements {
			switch e := inner.(type) {
			case *goslack.RichTextSectionTextElement:
				if e.Style != nil && (e.Style.Bold || e.Style.Italic || e.Style.Strike) {
					return "", false
				}
				if e.Style != nil && e.Style.Code {
					b.WriteByte('`')
					b.WriteString(e.Text)
					b.WriteByte('`')
				} else {
					b.WriteString(e.Text)
				}
			case *goslack.RichTextSectionUserElement,
				*goslack.RichTextSectionChannelElement,
				*goslack.RichTextSectionUserGroupElement,
				*goslack.RichTextSectionBroadcastElement:
				b.WriteString(mentionPlaceholder)
			case *goslack.RichTextSectionLinkElement:
				if e.Text == "" {
					fmt.Fprintf(&b, "<%s>", e.URL)
				} else {
					fmt.Fprintf(&b, "<%s|%s>", e.URL, e.Text)
				}
			case *goslack.RichTextSectionEmojiElement:
				fmt.Fprintf(&b, ":%s:", e.Name)
			default:
				return "", false
			}
		}
	}
	return b.String(), true
}

// matchesWithMentions reports whether storedText can be produced from the
// placeholder string by substituting each MENTION placeholder with some
// non-empty resolved value (e.g. "@Donna Dev" or "#general"). The check is
// structural: first and last literals must anchor to the ends of storedText
// (so mentions that happen to contain an interior literal don't swallow
// the boundary). Middle literals are matched greedily at their first
// occurrence — that's a simplification, but it's sound for the messages
// we see in practice.
func matchesWithMentions(storedText, placeholder string) bool {
	parts := strings.Split(placeholder, mentionPlaceholder)
	// Anchor first literal as prefix.
	if !strings.HasPrefix(storedText, parts[0]) {
		return false
	}
	storedText = storedText[len(parts[0]):]
	// Anchor last literal as suffix, when there is more than one part.
	last := parts[len(parts)-1]
	if !strings.HasSuffix(storedText, last) {
		return false
	}
	storedText = storedText[:len(storedText)-len(last)]
	middle := parts[1 : len(parts)-1]
	// storedText now consists of (mention)(middle[0])(mention)...(middle[n-1])(mention),
	// with len(middle)+1 mentions, each required to be at least one character.
	for _, lit := range middle {
		idx := strings.Index(storedText, lit)
		if idx < 1 {
			// idx < 0 → not found; idx == 0 → empty mention preceding.
			return false
		}
		storedText = storedText[idx+len(lit):]
	}
	// Final (trailing) mention must have at least one character.
	return len(storedText) >= 1
}

// richTextBucket fast-paths some cases to specific buckets before the main
// render step, so the report shows why BlocksEquivalentToText returned false.
func richTextBucket(rt *goslack.RichTextBlock, storedText string) (bucket, sample) {
	if len(rt.Elements) == 0 {
		return bucketUnsupported, sample{text: storedText}
	}
	// Check for unsupported element kinds or styled spans, to report them as
	// distinct buckets instead of lumping into "divergent".
	for _, el := range rt.Elements {
		sec, isSection := el.(*goslack.RichTextSection)
		if !isSection {
			return bucketUnsupported, sample{text: storedText}
		}
		for _, inner := range sec.Elements {
			if te, ok := inner.(*goslack.RichTextSectionTextElement); ok {
				if te.Style != nil && (te.Style.Bold || te.Style.Italic || te.Style.Strike) {
					return bucketStyledSpan, sample{text: storedText}
				}
			}
		}
	}
	return "", sample{}
}

func printReport(total int, counts map[bucket]int, samples map[bucket][]sample) {
	order := []bucket{
		bucketDirectMatch,
		bucketMentionMatch,
		bucketStyledSpan,
		bucketMultiBlock,
		bucketUnsupported,
		bucketNotRichText,
		bucketDivergentEntity,
		bucketDivergentEmoji,
		bucketDivergent,
		bucketAttachOrFileAlso,
		bucketNoBlocks,
	}
	fmt.Printf("Slack messages scanned:         %d\n", total)
	fmt.Println()
	withBlocks := 0
	for _, b := range order {
		if b == bucketNoBlocks || b == bucketAttachOrFileAlso {
			continue
		}
		withBlocks += counts[b]
	}
	fmt.Printf("Messages with blocks (no atts): %d\n", withBlocks)
	fmt.Println()
	for _, b := range order {
		n := counts[b]
		pct := 0.0
		if withBlocks > 0 && b != bucketNoBlocks && b != bucketAttachOrFileAlso {
			pct = float64(n) / float64(withBlocks) * 100
		}
		fmt.Printf("  %-28s %7d  %6.2f%%\n", b, n, pct)
	}
	fmt.Println()
	equivalent := counts[bucketDirectMatch] + counts[bucketMentionMatch]
	if withBlocks > 0 {
		fmt.Printf("Would-drop (direct + mention):  %d / %d  (%.2f%% of blocks)\n",
			equivalent, withBlocks, float64(equivalent)/float64(withBlocks)*100)
	}
	fmt.Println()

	// Show samples for the interesting buckets.
	interesting := []bucket{bucketDivergent, bucketDivergentEntity, bucketDivergentEmoji, bucketStyledSpan, bucketMultiBlock, bucketUnsupported, bucketNotRichText}
	for _, b := range interesting {
		ss := samples[b]
		if len(ss) == 0 {
			continue
		}
		fmt.Printf("=== %s samples (%d shown of %d total) ===\n", b, len(ss), counts[b])
		for i, s := range ss {
			fmt.Printf("[%d] %s  id=%s\n", i+1, trimPath(s.path), s.id)
			fmt.Printf("    text:   %s\n", truncate(s.text, 200))
			if s.render != "" {
				fmt.Printf("    render: %s\n", truncate(s.render, 200))
			}
		}
		fmt.Println()
	}

	// Workspace breakdown of direct matches.
	fmt.Println("=== per-workspace direct-match samples ===")
	ws := map[string]int{}
	for _, s := range samples[bucketDirectMatch] {
		ws[workspace(s.path)]++
	}
	keys := make([]string, 0, len(ws))
	for k := range ws {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-20s %d\n", k, ws[k])
	}
}

func trimPath(p string) string {
	home := os.Getenv("HOME")
	return strings.TrimPrefix(p, home)
}

func workspace(p string) string {
	parts := strings.Split(p, string(os.PathSeparator))
	for i, part := range parts {
		if part == "slack" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "?"
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
