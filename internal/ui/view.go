package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

// View renders the foundation screen without owning application state.
func View(width, height int) tea.View {
	content := titleStyle.Render("Kingdom") + "\n\n" +
		"Local workspace ready. Discovery and role assignment will arrive in v1.\n\n" +
		"Press q to quit."
	if width > 0 && height > 0 {
		content += fmt.Sprintf("\n\n%d×%d", width, height)
	}
	return tea.NewView(content)
}
