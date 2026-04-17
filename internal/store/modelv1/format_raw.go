package modelv1

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rawContent mirrors the structure serialized by ExtractRaw in the Slack
// listener. Only the fields needed for display formatting are included;
// unknown fields are silently ignored by json.Unmarshal.
type rawContent struct {
	Attachments []rawAttachment `json:"attachments,omitempty"`
	Files       []rawFile       `json:"files,omitempty"`
}

type rawAttachment struct {
	Fallback string              `json:"fallback"`
	Pretext  string              `json:"pretext"`
	Fields   []rawAttachmentField `json:"fields"`
}

type rawAttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type rawFile struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Mimetype  string `json:"mimetype"`
	Size      int64  `json:"size"`
	Permalink string `json:"permalink"`
}

// parseRawContent deserializes a Raw map back into typed structs via JSON
// round-trip, mirroring the serialization path in ExtractRaw.
func parseRawContent(raw map[string]any) (rawContent, bool) {
	if len(raw) == 0 {
		return rawContent{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return rawContent{}, false
	}
	var rc rawContent
	if err := json.Unmarshal(data, &rc); err != nil {
		return rawContent{}, false
	}
	if len(rc.Attachments) == 0 && len(rc.Files) == 0 {
		return rawContent{}, false
	}
	return rc, true
}

// formatRaw renders raw content (attachments, files) as indented display
// lines below the message text. Returns nil if there is nothing to render.
func formatRaw(raw map[string]any, indent string) []string {
	rc, ok := parseRawContent(raw)
	if !ok {
		return nil
	}
	var lines []string
	lines = append(lines, formatRawAttachments(rc.Attachments, indent)...)
	lines = append(lines, formatRawFiles(rc.Files, indent)...)
	return lines
}

// formatRawAttachments renders attachments (Jira unfurls, Jenkins
// notifications, etc.) as indented lines showing fallback text and fields.
func formatRawAttachments(atts []rawAttachment, indent string) []string {
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
func formatAttachmentFields(att rawAttachment) string {
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
func formatRawFiles(files []rawFile, indent string) []string {
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
			sizePart = humanSize(f.Size)
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
