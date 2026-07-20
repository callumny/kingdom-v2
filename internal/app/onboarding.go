package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/setup"
)

func (m Model) handleProvidersKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.providerCursor > 0 {
			m.providerCursor--
		}
	case "down", "j":
		if m.providerCursor+1 < len(m.workflow.Draft.Results) {
			m.providerCursor++
		}
	case " ", "space":
		if m.providerCursor >= 0 && m.providerCursor < len(m.workflow.Draft.Results) {
			id := m.workflow.Draft.Results[m.providerCursor].Endpoint.ID
			enabled := !m.workflow.Draft.ProviderEnabled(id)
			m.workflow.Err = m.workflow.Draft.SetProviderEnabled(id, enabled, setup.CurrentPlatform())
		}
	case "enter":
		if m.scanning {
			return m, nil
		}
		m.workflow.Err = nil
		if err := m.workflow.Continue(); err == nil {
			m.gate.Cancel()
			m.screen = m.workflow.State
		} else {
			m.workflow.Err = err
		}
	}
	return m, nil
}

func (m Model) handleModelsKey(key string) (tea.Model, tea.Cmd) {
	catalog := m.workflow.Draft.Catalog()
	switch key {
	case "up", "k":
		if m.modelCursor > 0 {
			m.modelCursor--
		}
	case "down", "j":
		if m.modelCursor+1 < len(catalog) {
			m.modelCursor++
		}
	case " ", "space":
		if m.modelCursor >= 0 && m.modelCursor < len(catalog) {
			m.workflow.Err = m.workflow.Draft.ToggleModel(catalog[m.modelCursor].Ref)
		}
	case "enter":
		if m.scanning {
			return m, nil
		}
		if err := m.workflow.Continue(); err != nil {
			m.workflow.Err = err
			return m, nil
		}
		m.workflow.Err = nil
		m.modelIndex = 0
		m.screen = m.workflow.State
	}
	return m, nil
}
