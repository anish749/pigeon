package wstui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	activeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dormantStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	resolvedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("219"))
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)
