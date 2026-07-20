package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
)

type LocalModelManager interface {
	Inspect(context.Context) []localmodels.Runtime
	StartAndWait(context.Context, localmodels.Kind, string) error
}

type localModelState struct {
	manager       LocalModelManager
	open          bool
	loading       bool
	starting      bool
	confirming    bool
	runtimeCursor int
	modelCursor   int
	generation    uint64
	runtimes      []localmodels.Runtime
	err           string
	cancel        context.CancelFunc
	preferred     *topology.Assignment
}

type localModelsMsg struct {
	generation uint64
	runtimes   []localmodels.Runtime
}

type localModelStartedMsg struct {
	generation uint64
	err        error
}

func (m *Model) openLocalModels() tea.Cmd {
	m.localModels.open = true
	m.localModels.runtimeCursor = 0
	m.localModels.modelCursor = 0
	m.localModels.confirming = false
	return m.inspectLocalModels()
}

func (m *Model) inspectLocalModels() tea.Cmd {
	if m.localModels.cancel != nil {
		m.localModels.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.localModels.cancel = cancel
	m.localModels.generation++
	generation := m.localModels.generation
	manager := m.localModels.manager
	m.localModels.loading = true
	m.localModels.err = ""
	return func() tea.Msg {
		return localModelsMsg{generation: generation, runtimes: manager.Inspect(ctx)}
	}
}

func (m *Model) startSelectedLocalModel() tea.Cmd {
	runtime, modelID, ok := m.selectedLocalModel()
	if !ok {
		m.localModels.err = "select an installed model"
		return nil
	}
	if runtime.Kind == localmodels.KindOllama && !runtime.Running {
		modelID = ""
	}
	if m.localModels.cancel != nil {
		m.localModels.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.localModels.cancel = cancel
	m.localModels.generation++
	generation := m.localModels.generation
	manager := m.localModels.manager
	m.localModels.starting = true
	m.localModels.confirming = false
	m.localModels.err = ""
	return func() tea.Msg {
		return localModelStartedMsg{generation: generation, err: manager.StartAndWait(ctx, runtime.Kind, modelID)}
	}
}

func (m Model) selectedLocalModel() (localmodels.Runtime, string, bool) {
	if m.localModels.runtimeCursor < 0 || m.localModels.runtimeCursor >= len(m.localModels.runtimes) {
		return localmodels.Runtime{}, "", false
	}
	runtime := m.localModels.runtimes[m.localModels.runtimeCursor]
	if !runtime.Installed {
		return runtime, "", false
	}
	if len(runtime.Models) == 0 {
		return runtime, "", runtime.Kind == localmodels.KindOllama && !runtime.Running
	}
	if m.localModels.modelCursor < 0 || m.localModels.modelCursor >= len(runtime.Models) {
		return runtime, "", false
	}
	return runtime, runtime.Models[m.localModels.modelCursor].ID, true
}

func (m Model) handleLocalModelsKey(key string) (Model, tea.Cmd) {
	if m.localModels.confirming {
		switch key {
		case "y":
			return m, m.startSelectedLocalModel()
		case "n", "esc":
			m.localModels.confirming = false
		}
		return m, nil
	}
	if m.localModels.loading || m.localModels.starting {
		if key == "esc" {
			if m.localModels.cancel != nil {
				m.localModels.cancel()
			}
			m.localModels.generation++
			m.localModels.open = false
			m.localModels.loading = false
			m.localModels.starting = false
		}
		return m, nil
	}
	switch key {
	case "esc", "ctrl+r":
		if m.localModels.cancel != nil {
			m.localModels.cancel()
		}
		m.localModels.generation++
		m.localModels.open = false
	case "r":
		return m, m.inspectLocalModels()
	case "right", "l":
		if len(m.localModels.runtimes) > 0 {
			m.localModels.runtimeCursor = (m.localModels.runtimeCursor + 1) % len(m.localModels.runtimes)
			m.localModels.modelCursor = 0
		}
	case "left", "h":
		if len(m.localModels.runtimes) > 0 {
			m.localModels.runtimeCursor = (m.localModels.runtimeCursor - 1 + len(m.localModels.runtimes)) % len(m.localModels.runtimes)
			m.localModels.modelCursor = 0
		}
	case "down", "j":
		if runtime, _, ok := m.selectedRuntime(); ok && len(runtime.Models) > 0 {
			m.localModels.modelCursor = (m.localModels.modelCursor + 1) % len(runtime.Models)
		}
	case "up", "k":
		if runtime, _, ok := m.selectedRuntime(); ok && len(runtime.Models) > 0 {
			m.localModels.modelCursor = (m.localModels.modelCursor - 1 + len(runtime.Models)) % len(runtime.Models)
		}
	case "s":
		runtime, _, ok := m.selectedLocalModel()
		if !ok {
			m.localModels.err = "select an installed model"
			return m, nil
		}
		if runtime.Kind == localmodels.KindOllama && runtime.Running {
			m.localModels.err = "Ollama is already ready; press Enter to assign the selected model"
			return m, nil
		}
		m.localModels.confirming = true
		m.localModels.err = ""
	case "enter":
		runtime, modelID, ok := m.selectedLocalModel()
		if !ok || !runtime.Running || modelID == "" {
			m.localModels.err = "start and load the selected model first"
			return m, nil
		}
		selected := false
		for _, model := range runtime.Models {
			if model.ID == modelID && (model.Loaded || runtime.Kind == localmodels.KindOllama) {
				selected = true
			}
		}
		if !selected {
			m.localModels.err = "start and load the selected model first"
			return m, nil
		}
		if !m.setup {
			m = m.startSetup()
		}
		m.localModels.open = false
		m.localModels.preferred = &topology.Assignment{EndpointID: runtime.Endpoint.ID, Model: modelID}
		return m.beginDiscovery()
	}
	return m, nil
}

func (m Model) selectedRuntime() (localmodels.Runtime, int, bool) {
	if m.localModels.runtimeCursor < 0 || m.localModels.runtimeCursor >= len(m.localModels.runtimes) {
		return localmodels.Runtime{}, 0, false
	}
	return m.localModels.runtimes[m.localModels.runtimeCursor], m.localModels.runtimeCursor, true
}

func (m *Model) focusPreferredModel() {
	if m.localModels.preferred == nil || m.workflow == nil {
		return
	}
	for index, option := range m.workflow.Draft.Catalog() {
		if option.Ref.EndpointID == m.localModels.preferred.EndpointID && option.Ref.ModelID == m.localModels.preferred.Model {
			m.modelCursor = index
			m.localModels.preferred = nil
			return
		}
	}
	m.localModels.err = fmt.Sprintf("selected model %s was not discovered", m.localModels.preferred.Model)
	m.workflow.Err = fmt.Errorf("selected model %s was not discovered", m.localModels.preferred.Model)
	m.localModels.preferred = nil
}

func (m Model) localModelsView() tea.View {
	return ui.LocalModelsView(m.width, m.height, m.localModels.runtimes, m.localModels.runtimeCursor, m.localModels.modelCursor, m.localModels.loading, m.localModels.starting, m.localModels.confirming, m.localModels.err)
}
