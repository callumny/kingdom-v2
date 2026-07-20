package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
)

func (m Model) handleProvidersKey(key string) (tea.Model, tea.Cmd) {
	if m.providerConfirming {
		switch key {
		case "y":
			m.providerConfirming = false
			m.providerInstalling = true
			return m, m.installSelectedProvider()
		case "n", "esc":
			m.providerConfirming = false
		}
		return m, nil
	}
	if m.providerInstalling {
		return m, nil
	}
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
	case "i":
		if m.installer == nil {
			m.workflow.Err = fmt.Errorf("provider installation is unavailable")
			return m, nil
		}
		kind, ok := m.selectedProviderKind()
		if !ok {
			m.workflow.Err = fmt.Errorf("select Ollama or MLX")
			return m, nil
		}
		platform := setup.CurrentPlatform()
		if (kind == localmodels.KindOllama && !platform.SupportsOllama()) || (kind == localmodels.KindMLX && !platform.SupportsMLX()) {
			m.workflow.Err = fmt.Errorf("%s is not supported on this computer", m.workflow.Draft.Results[m.providerCursor].Endpoint.Name)
			return m, nil
		}
		m.workflow.Err = nil
		m.providerNotice = ""
		m.providerConfirming = true
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

func (m Model) selectedProviderKind() (localmodels.Kind, bool) {
	if m.providerCursor < 0 || m.providerCursor >= len(m.workflow.Draft.Results) {
		return "", false
	}
	switch m.workflow.Draft.Results[m.providerCursor].Endpoint.ID {
	case setup.OllamaEndpointID:
		return localmodels.KindOllama, true
	case setup.MLXEndpointID:
		return localmodels.KindMLX, true
	default:
		return "", false
	}
}

func (m Model) installSelectedProvider() tea.Cmd {
	kind, ok := m.selectedProviderKind()
	if !ok || m.installer == nil {
		return func() tea.Msg {
			return providerInstalledMsg{kind: kind, err: fmt.Errorf("provider installation is unavailable")}
		}
	}
	installer := m.installer
	manager := m.localModels.manager
	platform := setup.CurrentPlatform()
	ollamaPort := m.workflow.Draft.Config.Providers.Ollama.Port
	return func() tea.Msg {
		ctx := context.Background()
		if err := installer.Install(ctx, kind, platform.OS, platform.Arch); err != nil {
			return providerInstalledMsg{kind: kind, err: err}
		}
		if kind == localmodels.KindOllama {
			if starter, ok := manager.(interface {
				ConfigureAndStart(context.Context, localmodels.Kind, int) error
			}); ok {
				if err := starter.ConfigureAndStart(ctx, kind, ollamaPort); err != nil {
					return providerInstalledMsg{kind: kind, err: err}
				}
			}
		}
		return providerInstalledMsg{kind: kind}
	}
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
