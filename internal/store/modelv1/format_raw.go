package modelv1

import (
	"fmt"
	"strings"

	goslack "github.com/slack-go/slack"

	slackmodel "github.com/anish749/pigeon/internal/listener/slack/model"
)

// formatRaw renders raw content (attachments, files) as indented display
// lines below the message text. Returns nil if there is nothing to render.
func formatRaw(raw map[string]any, indent string) []string {
	rc, err := slackmodel.FromSerializable(raw)
	if err != nil {
		return nil
	}
	var lines []string
	lines = append(lines, formatRawAttachments(rc.Attachments, indent)...)
	lines = append(lines, formatRawFiles(rc.Files, indent)...)
	return lines
}

// formatRawAttachments renders attachments (Jira unfurls, Jenkins
// notifications, etc.) as indented lines showing fallback text and fields.
func formatRawAttachments(atts []goslack.Attachment, indent string) []string {
	var lines []string
	for _, att := range atts {
		fallback := att.Fallback
		if fallback == "" {
			fallback = att.Pretext
		}
		if fallback == "" || fallback == "[no preview available]" {
			continue
		}
		for i, fline := range strings.Split(fallback, "\n") {
			if fline == "" {
				continue
			}
			if i == 0 {
				lines = append(lines, indent+"📎 "+fline)
			} else {
				lines = append(lines, indent+"   "+fline)
			}
		}
		if fields := formatAttachmentFields(att); fields != "" {
			lines = append(lines, indent+"   "+fields)
		}
	}
	return lines
}

// formatAttachmentFields renders attachment fields as "Key: Value · Key: Value".
// Fields whose value duplicates the fallback are skipped (some bots like
// Jenkins put identical content in both).
func formatAttachmentFields(att goslack.Attachment) string {
	var parts []string
	for _, f := range att.Fields {
		if f.Title == "" && f.Value == "" {
			continue
		}
		if f.Value == att.Fallback {
			continue
		}
		if f.Title != "" {
			parts = append(parts, f.Title+": "+f.Value)
		} else {
			parts = append(parts, f.Value)
		}
	}
	return strings.Join(parts, " · ")
}

// formatRawFiles renders file attachments as indented lines showing
// file name, MIME type, human-readable size, and permalink.
func formatRawFiles(files []goslack.File, indent string) []string {
	var lines []string
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = f.Title
		}
		if name == "" {
			name = "unnamed file"
		}
		var sizePart string
		if f.Size > 0 {
			sizePart = humanSize(int64(f.Size))
		}

		info := name
		if f.Mimetype != "" || sizePart != "" {
			var meta []string
			if f.Mimetype != "" {
				meta = append(meta, f.Mimetype)
			}
			if sizePart != "" {
				meta = append(meta, sizePart)
			}
			info += " (" + strings.Join(meta, ", ") + ")"
		}

		line := indent + "📄 " + info
		if f.Permalink != "" {
			line += "\n" + indent + "   " + f.Permalink
		}
		lines = append(lines, line)
	}
	return lines
}

// humanSize formats a byte count as a human-readable string.
func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
