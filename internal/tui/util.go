package tui

import "github.com/charmbracelet/lipgloss"

// truncate shortens s to fit width display columns, appending an ellipsis. A
// non-positive width yields an empty string.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	// Trim runes until it fits, leaving room for the ellipsis.
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
