package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/charmbracelet/x/ansi"
)

func MemoryView(width, height int, sessions []memory.Session, exchanges []memory.Exchange, cursor int, loading, confirming bool, loadError string) tea.View {
	lines := []string{"Kingdom Memory", ""}
	if loadError != "" {
		lines = append(lines, "Memory warning: "+loadError, "")
	}
	if len(sessions) == 0 {
		if loading {
			lines = append(lines, "Loading memory…")
		} else {
			lines = append(lines, "No saved sessions yet.")
		}
	} else {
		lines = append(lines, "Sessions:")
		for index, session := range sessions {
			pointer := "  "
			if index == cursor {
				pointer = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s  %d exchange(s)  %s", pointer, session.ID, session.ExchangeCount, session.UpdatedAt.Local().Format("2006-01-02 15:04")))
		}
		lines = append(lines, "", "Selected session:")
		if loading {
			lines = append(lines, "Loading memory…")
		} else if len(exchanges) == 0 {
			lines = append(lines, "No exchanges found.")
		} else {
			for _, exchange := range exchanges {
				lines = append(lines, "You: "+exchange.User, "King: "+exchange.Reply, "")
			}
		}
	}
	if confirming {
		lines = append(lines, "", "Delete this session? This cannot be undone. y confirm   n cancel")
	} else {
		lines = append(lines, "", "j/k move   d delete   r reload   Esc back   Ctrl+C quit")
	}
	return tea.NewView(fitMemoryView(strings.Join(lines, "\n"), width, height))
}

func fitMemoryView(content string, width, height int) string {
	lines := strings.Split(content, "\n")
	if width > 0 {
		for index, line := range lines {
			lines[index] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}
