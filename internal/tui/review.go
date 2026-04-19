// Package tui implements the outbox review terminal UI using Bubble Tea.
package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/timeutil"
	"github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	msgStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// RunReview starts the outbox review TUI. Blocks until quit.
func RunReview() error {
	p := tea.NewProgram(model{}, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type mode int

const (
	modeList mode = iota
	modeFeedback
)

type model struct {
	items    []*outbox.Item
	cursor   int
	mode     mode
	feedback string
	status   string
	err      error
	width    int
	height   int
}

// Bubble Tea messages
type (
	itemsMsg       []*outbox.Item
	actionDoneMsg  struct{ detail string }
	actionFailMsg  struct{ detail string }
	clearStatusMsg struct{}
	tickMsg        struct{}
)

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchItems(), tickEvery(time.Second))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case itemsMsg:
		m.items = []*outbox.Item(msg)
		m.err = nil
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}

	case actionDoneMsg:
		m.status = successStyle.Render("✓ " + msg.detail)
		return m, tea.Batch(m.fetchItems(), clearStatusAfter(3*time.Second))

	case actionFailMsg:
		m.status = errorStyle.Render("✗ " + msg.detail)
		return m, clearStatusAfter(30 * time.Second)

	case clearStatusMsg:
		m.status = ""

	case tickMsg:
		return m, tea.Batch(m.fetchItems(), tickEvery(time.Second))
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFeedback {
		return m.handleFeedbackKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "a":
		if len(m.items) > 0 {
			m.status = dimStyle.Render("Approving...")
			return m, m.approveItem(m.items[m.cursor].ID)
		}
	case "f":
		if len(m.items) > 0 && m.items[m.cursor].SessionID != "" {
			m.mode = modeFeedback
			m.feedback = ""
		}
	}
	return m, nil
}

func (m model) handleFeedbackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := tea.Key(msg)
	switch k.Type {
	case tea.KeyEscape:
		m.mode = modeList
		m.feedback = ""
	case tea.KeyEnter:
		if m.feedback != "" && len(m.items) > 0 {
			item := m.items[m.cursor]
			note := m.feedback
			m.mode = modeList
			m.feedback = ""
			m.status = dimStyle.Render("Sending feedback...")
			return m, m.sendFeedback(item.ID, note)
		}
	case tea.KeyBackspace:
		if len(m.feedback) > 0 {
			m.feedback = m.feedback[:len(m.feedback)-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		// Handles both single keystrokes and pasted text.
		m.feedback += string(k.Runes)
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	count := len(m.items)
	b.WriteString(titleStyle.Render(fmt.Sprintf("  Pigeon Outbox  %s", dimStyle.Render(fmt.Sprintf("%d pending", count)))))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v\n", m.err)))
		b.WriteString(helpStyle.Render("\n  q quit"))
		return b.String()
	}

	if count == 0 {
		b.WriteString(dimStyle.Render("  No pending items. Waiting for submissions..."))
		b.WriteString("\n\n")
		if m.status != "" {
			b.WriteString("  " + m.status + "\n\n")
		}
		b.WriteString(helpStyle.Render("  q quit"))
		return b.String()
	}

	// Item list
	for i, item := range m.items {
		age := time.Since(item.CreatedAt).Truncate(time.Second)
		summary := itemSummary(item)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("● %s", summary)))
			b.WriteString("  " + dimStyle.Render(timeutil.FormatAge(age)))
		} else {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s  %s", summary, timeutil.FormatAge(age))))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Detail pane
	if m.cursor < count {
		b.WriteString(m.renderDetail(m.items[m.cursor]))
		b.WriteString("\n")
	}

	if m.status != "" {
		b.WriteString("  " + m.status + "\n")
	}

	b.WriteString("\n")
	if m.mode == modeFeedback {
		b.WriteString("  " + titleStyle.Render("Feedback:") + " " + m.feedback + "█\n")
		b.WriteString(helpStyle.Render("  enter send  esc cancel"))
	} else {
		help := "  a approve  f feedback  j/k navigate  q quit"
		if m.cursor < count && m.items[m.cursor].SessionID == "" {
			help = "  a approve  j/k navigate  q quit  " + dimStyle.Render("(feedback unavailable — no session)")
		}
		b.WriteString(helpStyle.Render(help))
	}
	return b.String()
}

func (m model) renderDetail(item *outbox.Item) string {
	var resolved api.ResolvedSendRequest
	if err := json.Unmarshal(item.Payload, &resolved); err != nil {
		return "  " + dimStyle.Render("(cannot parse payload)")
	}
	req := resolved.SendRequest

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  To: %s\n", resolved.ResolvedTarget()))
	b.WriteString(fmt.Sprintf("  On: %s / %s\n", req.Platform, req.Account))
	b.WriteString(fmt.Sprintf("  From: %s\n", sendIdentity(req)))
	if req.Thread != "" {
		b.WriteString(fmt.Sprintf("  Thread: %s\n", req.Thread))
	}
	b.WriteString("\n")

	maxWidth := m.width - 6
	if maxWidth < 40 {
		maxWidth = 40
	}
	box := msgStyle.Width(maxWidth).Render(resolved.FinalMessage())
	for _, line := range strings.Split(box, "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// --- Commands ---

func (m model) fetchItems() tea.Cmd {
	return func() tea.Msg {
		items, err := doGet()
		if err != nil {
			return itemsMsg(nil)
		}
		return itemsMsg(items)
	}
}

func (m model) approveItem(id string) tea.Cmd {
	return func() tea.Msg {
		body, err := json.Marshal(outbox.ActionRequest{ID: id, Action: "approve"})
		if err != nil {
			return actionFailMsg{"marshal request: " + err.Error()}
		}
		result, err := doPost("http://pigeon/api/outbox/action", body)
		if err != nil {
			return actionFailMsg{err.Error()}
		}
		if ok, _ := result["ok"].(bool); ok {
			return actionDoneMsg{"Approved and sent"}
		}
		detail, _ := result["error"].(string)
		return actionFailMsg{detail}
	}
}

func (m model) sendFeedback(id, note string) tea.Cmd {
	return func() tea.Msg {
		body, err := json.Marshal(outbox.ActionRequest{ID: id, Action: "feedback", Note: note})
		if err != nil {
			return actionFailMsg{"marshal request: " + err.Error()}
		}
		result, err := doPost("http://pigeon/api/outbox/action", body)
		if err != nil {
			return actionFailMsg{err.Error()}
		}
		if ok, _ := result["ok"].(bool); ok {
			return actionDoneMsg{"Feedback sent to session"}
		}
		detail, _ := result["error"].(string)
		return actionFailMsg{detail}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

// --- HTTP helpers ---

func doGet() ([]*outbox.Item, error) {
	resp, err := daemonclient.DefaultPgnHTTPClient.Get("http://pigeon/api/outbox")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var items []*outbox.Item
	json.NewDecoder(resp.Body).Decode(&items)
	return items, nil
}

func doPost(url string, body []byte) (map[string]any, error) {
	resp, err := daemonclient.DefaultPgnHTTPClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// itemSummary derives a one-line display string from the outbox item's payload.
func itemSummary(item *outbox.Item) string {
	var resolved api.ResolvedSendRequest
	if err := json.Unmarshal(item.Payload, &resolved); err != nil {
		return "(unknown)"
	}
	req := resolved.SendRequest
	msg := resolved.FinalMessage()
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	target := resolved.ResolvedTarget()
	// Slack can send as either bot or user, so call out the identity.
	// WhatsApp always sends as the user — no need to clutter the line.
	if req.Platform == "slack" {
		return fmt.Sprintf("%s → %s (from %s): %s", req.Platform, target, sendIdentity(req), msg)
	}
	return fmt.Sprintf("%s → %s: %s", req.Platform, target, msg)
}

// sendIdentity returns "user" or "pigeon" — the sender identity for an
// outbound message. WhatsApp always sends as the user (no bot identity).
func sendIdentity(req api.SendRequest) string {
	if req.Platform == "whatsapp" || req.Via == modelv1.ViaPigeonAsUser {
		return "user"
	}
	return "pigeon"
}
