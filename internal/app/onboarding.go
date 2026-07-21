package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func (m Model) handleProvidersKey(key string) (tea.Model, tea.Cmd) {
	if m.providerConfirming {
		switch key {
		case "y":
			m.providerConfirming = false
			m.providerInstalling = true
			return m.beginProviderInstall()
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
		endpointID := setup.OllamaEndpointID
		if kind == localmodels.KindMLX {
			endpointID = setup.MLXEndpointID
		}
		if m.workflow.Draft.ProviderReady(endpointID) {
			m.workflow.Err = nil
			m.providerNotice = "This provider is already ready."
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
			return m.beginModelInventory()
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

func (m Model) beginProviderInstall() (tea.Model, tea.Cmd) {
	kind, ok := m.selectedProviderKind()
	if !ok || m.installer == nil {
		m.providerInstalling = false
		m.workflow.Err = fmt.Errorf("provider installation is unavailable")
		return m, nil
	}
	installer := m.installer
	manager := m.localModels.manager
	platform := setup.CurrentPlatform()
	ollamaPort := m.workflow.Draft.Config.Providers.Ollama.Port
	m.providerInstallGen++
	generation := m.providerInstallGen
	channel := make(chan providerInstallEvent, 16)
	m.providerInstallCh = channel
	m.providerProgress = localmodels.InstallProgress{Completed: 0, Total: 1, Message: "Starting provider setup"}
	endpointID := setup.OllamaEndpointID
	if kind == localmodels.KindMLX {
		endpointID = setup.MLXEndpointID
	}
	m.workflow.Draft.SetProviderReady(endpointID, false)
	go func() {
		defer close(channel)
		ctx := context.Background()
		report := func(progress localmodels.InstallProgress) {
			value := progress
			channel <- providerInstallEvent{progress: &value}
		}
		if err := installer.InstallWithProgress(ctx, kind, platform.OS, platform.Arch, report); err != nil {
			channel <- providerInstallEvent{done: true, err: err}
			return
		}
		if kind == localmodels.KindOllama {
			if starter, ok := manager.(interface {
				ConfigureAndStart(context.Context, localmodels.Kind, int) error
			}); ok {
				if err := starter.ConfigureAndStart(ctx, kind, ollamaPort); err != nil {
					channel <- providerInstallEvent{done: true, err: err}
					return
				}
			}
		}
		channel <- providerInstallEvent{done: true}
	}()
	return m, m.nextProviderInstallEventWithGeneration(kind, generation)
}

func (m Model) nextProviderInstallEvent(kind localmodels.Kind) tea.Cmd {
	return m.nextProviderInstallEventWithGeneration(kind, m.providerInstallGen)
}

func (m Model) nextProviderInstallEventWithGeneration(kind localmodels.Kind, generation uint64) tea.Cmd {
	channel := m.providerInstallCh
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			event = providerInstallEvent{done: true}
		}
		return providerInstallEventMsg{generation: generation, kind: kind, event: event}
	}
}

func (m Model) handleModelsKey(key string) (tea.Model, tea.Cmd) {
	if m.modelInventoryLoad {
		return m, nil
	}
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

func (m Model) beginModelInventory() (tea.Model, tea.Cmd) {
	if m.localModels.manager == nil {
		return m, nil
	}
	m.modelInventoryGen++
	generation := m.modelInventoryGen
	m.modelInventoryLoad = true
	m.modelCursor = 0
	m.workflow.Err = nil
	m.workflow.Draft.ReplaceCatalog([]setup.ModelOption{})
	manager := m.localModels.manager
	return m, func() tea.Msg {
		return modelInventoryMsg{generation: generation, runtimes: manager.Inspect(context.Background())}
	}
}

func (m Model) installedModelOptions(runtimes []localmodels.Runtime) []setup.ModelOption {
	endpoints := make(map[string]topology.Endpoint)
	for _, endpoint := range m.workflow.Draft.Config.Topology.Endpoints {
		endpoints[endpoint.ID] = endpoint
	}
	options := make([]setup.ModelOption, 0)
	for _, runtime := range runtimes {
		endpointID := runtime.Endpoint.ID
		if endpointID == "" {
			switch runtime.Kind {
			case localmodels.KindOllama:
				endpointID = setup.OllamaEndpointID
			case localmodels.KindMLX:
				endpointID = setup.MLXEndpointID
			}
		}
		if !m.workflow.Draft.ProviderEnabled(endpointID) {
			continue
		}
		endpoint := runtime.Endpoint
		if configured, exists := endpoints[endpointID]; exists {
			endpoint = configured
		}
		for _, model := range runtime.Models {
			options = append(options, setup.ModelOption{
				Ref:       setup.ModelRef{EndpointID: endpointID, ModelID: model.ID},
				Endpoint:  endpoint,
				Installed: true,
			})
		}
	}
	return options
}
