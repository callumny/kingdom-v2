package app

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/modelcatalog"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func (m Model) handleModelsKey(key string) (tea.Model, tea.Cmd) {
	if m.modelInventoryLoading || m.modelRemoveActive {
		return m, nil
	}
	if m.modelRemoveConfirming {
		switch key {
		case "y", "enter":
			return m.beginModelRemoval()
		case "n", "esc":
			m.modelRemoveConfirming = false
			m.modelRemoveTarget = setup.ModelOption{}
		}
		return m, nil
	}
	if m.modelDownloadConfirming {
		switch key {
		case "y", "enter":
			m.modelDownloadConfirming = false
			return m.beginModelDownloads()
		case "n", "esc":
			m.modelDownloadConfirming = false
		}
		return m, nil
	}
	if key == "/" && !m.modelSearchActive {
		m.modelSearchActive = true
		m.modelSearchWarning = ""
		m.modelRemoveNotice = ""
		return m, nil
	}
	if m.modelSearchActive {
		switch key {
		case "esc":
			if m.modelQuery == "" {
				m.modelSearchActive = false
				return m, nil
			}
			m.modelQuery = ""
			return m.beginModelSearch()
		case "enter":
			m.modelSearchActive = false
			return m, nil
		case "backspace":
			if m.modelQuery != "" {
				_, size := utf8.DecodeLastRuneInString(m.modelQuery)
				m.modelQuery = m.modelQuery[:len(m.modelQuery)-size]
			}
			return m.beginModelSearch()
		case "up", "k", "down", "j", " ", "space":
			// Search remains focused while the result cursor moves below.
		default:
			if len([]rune(key)) == 1 {
				m.modelQuery += key
				return m.beginModelSearch()
			}
			return m, nil
		}
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
	case "d":
		m.modelRemoveNotice = ""
		if m.modelCursor < 0 || m.modelCursor >= len(catalog) {
			return m, nil
		}
		option := catalog[m.modelCursor]
		if !option.Installed {
			m.workflow.Err = fmt.Errorf("only installed models can be uninstalled")
			return m, nil
		}
		if m.modelRemover == nil {
			m.workflow.Err = fmt.Errorf("model uninstaller is unavailable")
			return m, nil
		}
		m.modelRemoveTarget = option
		m.modelRemoveConfirming = true
		m.modelRemoveNotice = ""
		m.workflow.Err = nil
	case "enter":
		if len(m.workflow.Draft.SelectedModels()) == 0 {
			m.workflow.Err = fmt.Errorf("select at least one model")
			return m, nil
		}
		if len(m.workflow.Draft.PendingDownloads()) > 0 {
			m.modelDownloadConfirming = true
			m.workflow.Err = nil
			return m, nil
		}
		return m.advanceFromModels()
	}
	return m, nil
}

func (m Model) advanceFromModels() (Model, tea.Cmd) {
	if err := m.workflow.Continue(); err != nil {
		m.workflow.Err = err
		return m, nil
	}
	m.workflow.Err = nil
	m.modelIndex = 0
	m.screen = m.workflow.State
	m.cancelModelSearch()
	return m.beginImmediateWizard(m.modelsReturnToReady)
}

func (m Model) reopenModels() (Model, tea.Cmd) {
	m.workflow = &setup.Workflow{State: setup.StateModels, Draft: setup.NewDraft(m.config, m.defaults), Previous: m.config}
	m.setup = true
	m.screen = setup.StateModels
	m.modelsReturnToReady = true
	m.chatError = ""
	next, command := m.beginModelInventory()
	return next.(Model), command
}

func (m Model) beginModelInventory() (tea.Model, tea.Cmd) {
	if m.localModels.manager == nil {
		return m, nil
	}
	m.modelInventoryGen++
	generation := m.modelInventoryGen
	m.modelInventoryLoading = true
	m.modelCursor = 0
	m.modelQuery = ""
	m.modelSearchActive = false
	m.modelDownloadConfirming = false
	m.modelRemoveConfirming = false
	m.modelRemoveTarget = setup.ModelOption{}
	m.cancelModelSearch()
	m.workflow.Err = nil
	m.workflow.Draft.ReplaceCatalog([]setup.ModelOption{})
	manager := m.localModels.manager
	return m, func() tea.Msg {
		return modelInventoryMsg{generation: generation, runtimes: manager.Inspect(context.Background())}
	}
}

func (m Model) beginModelSearch() (tea.Model, tea.Cmd) {
	m.cancelModelSearch()
	m.modelSearchGen++
	generation := m.modelSearchGen
	m.modelSearchWarning = ""
	m.replaceVisibleModels(nil)
	if strings.TrimSpace(m.modelQuery) == "" || m.modelSearch == nil {
		m.modelSearching = false
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.modelSearchCancel = cancel
	m.modelSearching = true
	query := m.modelQuery
	searcher := m.modelSearch
	providers := m.enabledModelProviders()
	return m, func() tea.Msg {
		type providerResult struct {
			provider modelcatalog.Provider
			models   []modelcatalog.Model
			err      error
		}
		results := make(chan providerResult, len(providers))
		for _, provider := range providers {
			go func(provider modelcatalog.Provider) {
				models, err := searcher.Search(ctx, provider, query, 10)
				results <- providerResult{provider: provider, models: models, err: err}
			}(provider)
		}
		message := modelSearchMsg{generation: generation}
		for range providers {
			result := <-results
			if result.err != nil {
				if ctx.Err() == nil {
					message.warnings = append(message.warnings, fmt.Sprintf("%s search: %v", providerLabel(result.provider), result.err))
				}
				continue
			}
			message.models = append(message.models, result.models...)
		}
		return message
	}
}

func (m *Model) cancelModelSearch() {
	if m.modelSearchCancel != nil {
		m.modelSearchCancel()
	}
	m.modelSearchCancel = nil
	m.modelSearching = false
}

func (m Model) enabledModelProviders() []modelcatalog.Provider {
	providers := make([]modelcatalog.Provider, 0, 2)
	if m.workflow.Draft.Config.Providers.Ollama.Enabled {
		providers = append(providers, modelcatalog.Ollama)
	}
	if m.workflow.Draft.Config.Providers.MLX.Enabled {
		providers = append(providers, modelcatalog.MLX)
	}
	return providers
}

func (m *Model) replaceVisibleModels(remote []modelcatalog.Model) {
	installed := make([]modelcatalog.Model, 0, len(m.installedModels))
	installedOptions := make(map[setup.ModelRef]setup.ModelOption, len(m.installedModels))
	for _, option := range m.installedModels {
		provider, ok := providerForEndpoint(option.Ref.EndpointID)
		if !ok {
			continue
		}
		installed = append(installed, modelcatalog.Model{Provider: provider, ID: option.Ref.ModelID, Installed: true})
		installedOptions[option.Ref] = option
	}
	merged := modelcatalog.MergeAndFilter(installed, remote, m.modelQuery, 10)
	options := make([]setup.ModelOption, 0, len(merged))
	for _, model := range merged {
		endpointID := endpointForProvider(model.Provider)
		ref := setup.ModelRef{EndpointID: endpointID, ModelID: model.ID}
		if installedOption, exists := installedOptions[ref]; exists {
			options = append(options, installedOption)
			continue
		}
		options = append(options, setup.ModelOption{Ref: ref, Endpoint: m.configuredEndpoint(endpointID), Installed: false})
	}
	m.workflow.Draft.ReplaceCatalog(options)
}

func (m Model) configuredEndpoint(endpointID string) topology.Endpoint {
	for _, endpoint := range m.workflow.Draft.Config.Topology.Endpoints {
		if endpoint.ID == endpointID {
			return endpoint
		}
	}
	return topology.Endpoint{ID: endpointID, Name: providerLabel(providerFromEndpoint(endpointID))}
}

func providerForEndpoint(endpointID string) (modelcatalog.Provider, bool) {
	switch endpointID {
	case setup.OllamaEndpointID:
		return modelcatalog.Ollama, true
	case setup.MLXEndpointID:
		return modelcatalog.MLX, true
	default:
		return "", false
	}
}

func providerFromEndpoint(endpointID string) modelcatalog.Provider {
	provider, _ := providerForEndpoint(endpointID)
	return provider
}

func endpointForProvider(provider modelcatalog.Provider) string {
	if provider == modelcatalog.MLX {
		return setup.MLXEndpointID
	}
	return setup.OllamaEndpointID
}

func providerLabel(provider modelcatalog.Provider) string {
	if provider == modelcatalog.MLX {
		return "MLX"
	}
	return "Ollama"
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
				Ref:           setup.ModelRef{EndpointID: endpointID, ModelID: model.ID},
				Endpoint:      endpoint,
				Installed:     true,
				SizeBytes:     model.SizeBytes,
				Family:        model.Family,
				ParameterSize: model.ParameterSize,
				Quantization:  model.Quantization,
			})
		}
	}
	return options
}
