package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

// View renders the foundation screen without owning application state.
func View(width, height int, setupRequired bool) tea.View {
	status := "Configuration ready."
	if setupRequired {
		status = "Setup required. Model discovery and role assignment will arrive in the next stage."
	}
	content := titleStyle.Render("Kingdom") + "\n\n" + status + "\n\nPress q to quit."
	if width > 0 && height > 0 {
		content += fmt.Sprintf("\n\n%d×%d", width, height)
	}
	return tea.NewView(content)
}
