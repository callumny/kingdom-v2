package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/ui"
)

// Model is the top-level application model. Domain orchestration is intentionally absent.
type Model struct {
	width  int
	height int
}

func New() Model { return Model{} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() tea.View { return ui.View(m.width, m.height) }
