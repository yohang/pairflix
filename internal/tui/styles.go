package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))
)

// pane renders content in a bordered box with a title, clamped to width.
func pane(title, content string, width int) string {
	inner := width - 4 // border + padding
	if inner < 1 {
		inner = 1
	}

	body := titleStyle.Render(title) + "\n" + content

	return paneStyle.Width(width - 2).MaxWidth(width).Render(clampLines(body, inner))
}

// clampLines truncates each line of s to width columns.
func clampLines(s string, width int) string {
	var out strings.Builder

	for i, line := range splitLines(s) {
		if i > 0 {
			out.WriteByte('\n')
		}

		out.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(line))
	}

	return out.String()
}

func splitLines(s string) []string {
	var (
		lines []string
		cur   []rune
	)

	for _, r := range s {
		if r == '\n' {
			lines = append(lines, string(cur))
			cur = cur[:0]

			continue
		}

		cur = append(cur, r)
	}

	return append(lines, string(cur))
}
