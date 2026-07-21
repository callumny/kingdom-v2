package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	royalText    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f4efe3"))
	royalMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#aaa09a"))
	royalRule    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c4251"))
	royalGold    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e8bd61"))
	royalBrand   = royalGold.Bold(true)
	royalCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("#89c7d6"))
	royalGreen   = lipgloss.NewStyle().Foreground(lipgloss.Color("#78d19a"))
	royalRed     = lipgloss.NewStyle().Foreground(lipgloss.Color("#d46a6a"))
	royalPointer = royalGold.Bold(true)
	royalBadge   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f4efe3")).Background(lipgloss.Color("#3b3341")).Bold(true).Padding(0, 1)
	royalReady   = lipgloss.NewStyle().Foreground(lipgloss.Color("#17231b")).Background(lipgloss.Color("#78d19a")).Bold(true).Padding(0, 1)
	royalPending = lipgloss.NewStyle().Foreground(lipgloss.Color("#241d10")).Background(lipgloss.Color("#e8bd61")).Bold(true).Padding(0, 1)
)

func renderRoyalShell(width, height int, progress string, body []string, footer string) string {
	contentWidth := width - 4
	if width <= 0 {
		contentWidth = 92
	}
	if contentWidth > 96 {
		contentWidth = 96
	}
	if contentWidth < 1 {
		contentWidth = 1
	}

	header := royalBrand.Render("♛ KINGDOM")
	status := royalMuted.Render("LOCAL · SETUP")
	if gap := contentWidth - ansi.StringWidth(header) - ansi.StringWidth(status); gap > 1 {
		header += strings.Repeat(" ", gap) + status
	}
	prefix := []string{header, royalRule.Render(strings.Repeat("─", contentWidth)), ""}
	if progress != "" {
		prefix = append(prefix, progress, "")
	}
	suffix := []string{"", royalRule.Render(strings.Repeat("─", contentWidth)), footer}

	if height > 0 {
		available := height - len(prefix) - len(suffix)
		if available < 0 {
			lines := append(append([]string{}, prefix...), body...)
			lines = append(lines, suffix...)
			if len(lines) > height {
				lines = lines[:height]
			}
			return fitRoyalLines(lines, width, contentWidth)
		}
		if len(body) > available {
			body = body[:available]
		}
		for len(body) < available {
			body = append(body, "")
		}
	}

	lines := append(append([]string{}, prefix...), body...)
	lines = append(lines, suffix...)
	return fitRoyalLines(lines, width, contentWidth)
}

func fitRoyalLines(lines []string, terminalWidth, contentWidth int) string {
	leftPadding := 0
	if terminalWidth > contentWidth {
		leftPadding = (terminalWidth - contentWidth) / 2
	}
	padding := strings.Repeat(" ", leftPadding)
	for index, line := range lines {
		lines[index] = padding + ansi.Truncate(line, contentWidth, "")
	}
	return strings.Join(lines, "\n")
}

func setupProgress(active int) string {
	labels := []string{"1 Providers", "2 Models", "3 Roles", "4 Review"}
	parts := make([]string, 0, len(labels)*2-1)
	for index, label := range labels {
		style := royalMuted
		if index+1 == active {
			style = royalBrand
		}
		parts = append(parts, style.Render(label))
		if index+1 < len(labels) {
			parts = append(parts, royalRule.Render("─"))
		}
	}
	return strings.Join(parts, "  ")
}

func styledParagraph(text string, width int, style lipgloss.Style) []string {
	plain := wrapWords(text, width)
	for index := range plain {
		plain[index] = style.Render(plain[index])
	}
	return plain
}

func wrapWords(text string, width int) []string {
	if width < 1 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if ansi.StringWidth(lines[last])+1+ansi.StringWidth(word) <= width {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	return lines
}
