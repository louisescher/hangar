package tui

import "github.com/charmbracelet/lipgloss"

// theme holds the lipgloss styles shared across screens. Colors are adaptive so
// the UI is legible on light and dark terminals; NO_COLOR is honored by
// lipgloss's underlying renderer.
type theme struct {
	title       lipgloss.Style
	subtitle    lipgloss.Style
	faint       lipgloss.Style
	accent      lipgloss.Style
	selected    lipgloss.Style
	cursor      lipgloss.Style
	check       lipgloss.Style
	partial     lipgloss.Style
	errorText   lipgloss.Style
	okText      lipgloss.Style
	border      lipgloss.Style
	borderFocus lipgloss.Style
	help        lipgloss.Style
	badge       lipgloss.Style
}

func newTheme() theme {
	accent := lipgloss.AdaptiveColor{Light: "#5b21b6", Dark: "#a78bfa"}
	faint := lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	good := lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34d399"}
	bad := lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	return theme{
		title:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		subtitle:    lipgloss.NewStyle().Foreground(faint),
		faint:       lipgloss.NewStyle().Foreground(faint),
		accent:      lipgloss.NewStyle().Foreground(accent),
		selected:    lipgloss.NewStyle().Bold(true),
		cursor:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		check:       lipgloss.NewStyle().Foreground(good).Bold(true),
		partial:     lipgloss.NewStyle().Foreground(accent),
		errorText:   lipgloss.NewStyle().Foreground(bad).Bold(true),
		okText:      lipgloss.NewStyle().Foreground(good),
		border:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint),
		borderFocus: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		help:        lipgloss.NewStyle().Foreground(faint),
		badge:       lipgloss.NewStyle().Foreground(accent).Bold(true),
	}
}
