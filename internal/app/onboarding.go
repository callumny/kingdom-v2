package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/setup"
)

func (m Model) handleWelcomeKey(key string) (tea.Model, tea.Cmd) {
	if key == "enter" {
		if err := m.workflow.Continue(); err == nil {
			m.screen = m.workflow.State
		}
	}
	return m, nil
}

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
		m.toggleSelectedProvider()
	case "enter":
		if m.scanning {
			return m, nil
		}
		if m.selectedModelCount() == 0 {
			if m.localModels.manager != nil {
				return m, m.openLocalModels()
			}
			m.workflow.Err = fmt.Errorf("select at least one provider with an available model")
			return m, nil
		}
		m.workflow.Err = nil
		if err := m.workflow.Continue(); err == nil {
			m.gate.Cancel()
			m.screen = m.workflow.State
		}
	}
	return m, nil
}

func (m *Model) syncProviderSelection(results []setup.EndpointResult) {
	if m.providerSelected == nil {
		m.providerSelected = make(map[string]bool, len(results))
	}
	for _, result := range results {
		if len(result.Models) == 0 {
			m.providerSelected[result.Endpoint.ID] = false
			continue
		}
		if _, exists := m.providerSelected[result.Endpoint.ID]; !exists {
			m.providerSelected[result.Endpoint.ID] = true
		}
	}
	if len(results) == 0 {
		m.providerCursor = 0
	} else if m.providerCursor >= len(results) {
		m.providerCursor = len(results) - 1
	}
}

func (m *Model) toggleSelectedProvider() {
	if m.workflow == nil || m.providerCursor < 0 || m.providerCursor >= len(m.workflow.Draft.Results) {
		return
	}
	result := m.workflow.Draft.Results[m.providerCursor]
	if len(result.Models) == 0 {
		m.workflow.Err = fmt.Errorf("%s has no available models", result.Endpoint.Name)
		return
	}
	if m.providerSelected == nil {
		m.providerSelected = make(map[string]bool)
	}
	m.providerSelected[result.Endpoint.ID] = !m.providerSelected[result.Endpoint.ID]
	m.workflow.Err = nil
}

func (m Model) selectedProviderResults() []setup.EndpointResult {
	if m.workflow == nil {
		return nil
	}
	if m.providerSelected == nil {
		return m.workflow.Draft.Results
	}
	results := make([]setup.EndpointResult, 0, len(m.workflow.Draft.Results))
	for _, result := range m.workflow.Draft.Results {
		if m.providerSelected[result.Endpoint.ID] {
			results = append(results, result)
		}
	}
	return results
}

func (m Model) selectedModelCount() int {
	count := 0
	for _, result := range m.selectedProviderResults() {
		count += len(result.Models)
	}
	return count
}
