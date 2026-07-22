package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
	"github.com/callumny/kingdom/internal/wizard"
)

type wizardPreparedMsg struct {
	generation uint64
	model      setup.ModelOption
	err        error
}

type wizardReplyMsg struct {
	generation uint64
	reply      wizard.Reply
	err        error
}

type wizardApplyMsg struct {
	generation uint64
	config     config.Config
	err        error
}

type runtimeWarmResult struct {
	Config config.Config
	Err    error
}

type wizardWarmMsg struct {
	generation uint64
	result     runtimeWarmResult
}

func (m Model) beginImmediateWizard(returnToReady bool) (Model, tea.Cmd) {
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		m.workflow.Err = err
		return m, nil
	}
	model, ok := selectedWizardModel(m.workflow.Draft)
	if !ok {
		m.workflow.Err = fmt.Errorf("no selected model can run the Wizard")
		return m, nil
	}
	if m.wizardCancel != nil {
		m.wizardCancel()
	}
	m.wizardGeneration++
	generation := m.wizardGeneration
	m.wizardModel = model
	m.wizardReturnToReady = returnToReady
	m.startWizard()
	m.wizardMessages = []string{"Wizard: I prepared sensible defaults and selected your Worker model for a fast setup conversation. You can apply them now or ask for one change."}
	m.wizardReady = true
	m.workflow.Err = nil
	var prepareCommand tea.Cmd
	if m.prepareWizard != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.wizardCancel = cancel
		m.wizardPreparing = true
		m.wizardEngine = nil
		prepare := m.prepareWizard
		cfg := m.workflow.Draft.Config
		prepareCommand = func() tea.Msg {
			defer cancel()
			prepared, err := prepare(ctx, cfg, model)
			return wizardPreparedMsg{generation: generation, model: prepared, err: err}
		}
	}
	if prepareCommand != nil {
		return m, prepareCommand
	}
	return m.beginWizardWarmup()
}

func (m Model) beginWizardWarmup() (Model, tea.Cmd) {
	if m.warmRun == nil {
		return m, nil
	}
	if m.wizardWarmCancel != nil {
		m.wizardWarmCancel()
	}
	m.wizardWarmGeneration++
	generation := m.wizardWarmGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.wizardWarmCancel = cancel
	m.wizardWarming = true
	cfg := m.workflow.Draft.Config
	m.runtimeWarmSignature = configSignature(cfg)
	results := make(chan runtimeWarmResult, 1)
	m.runtimeWarm = results
	warm := m.warmRun
	return m, func() tea.Msg {
		runtimeConfig, err := warm(ctx, cfg)
		result := runtimeWarmResult{Config: runtimeConfig, Err: err}
		results <- result
		close(results)
		return wizardWarmMsg{generation: generation, result: result}
	}
}

func configSignature(cfg config.Config) string {
	endpointIDs := map[string]bool{
		cfg.Topology.Roles.King.EndpointID:   true,
		cfg.Topology.Roles.Worker.EndpointID: true,
	}
	if cfg.CouncilEnabled {
		endpointIDs[cfg.Topology.Roles.Council.EndpointID] = true
	}
	endpoints := make([]topology.Endpoint, 0, len(endpointIDs))
	for _, endpoint := range cfg.Topology.Endpoints {
		if endpointIDs[endpoint.ID] {
			endpoints = append(endpoints, endpoint)
		}
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	operational := struct {
		Version           int
		Providers         config.Providers
		CouncilEnabled    bool
		CouncilSize       int
		WorkerConcurrency int
		Roles             topology.Roles
		Endpoints         []topology.Endpoint
	}{
		Version:           cfg.Version,
		Providers:         cfg.Providers,
		CouncilEnabled:    cfg.CouncilEnabled,
		CouncilSize:       cfg.CouncilSize,
		WorkerConcurrency: cfg.WorkerConcurrency,
		Roles:             cfg.Topology.Roles,
		Endpoints:         endpoints,
	}
	encoded, _ := json.Marshal(operational)
	return string(encoded)
}

func selectedWizardModel(draft setup.Draft) (setup.ModelOption, bool) {
	worker := draft.Config.Topology.Roles.Worker
	for _, option := range draft.SelectedModels() {
		if option.Ref.Assignment() == worker {
			return option, true
		}
	}
	return setup.ModelOption{}, false
}

func (m Model) reopenWizard() (Model, tea.Cmd) {
	draft, err := configuredWizardDraft(m.config, m.defaults)
	if err != nil {
		m.chatError = err.Error()
		return m, nil
	}
	m.workflow = &setup.Workflow{State: setup.StateWizard, Draft: draft, Previous: m.config}
	m.setup = true
	m.screen = setup.StateWizard
	m.chatError = ""
	return m.beginImmediateWizard(true)
}

func configuredWizardDraft(cfg config.Config, defaults []topology.Endpoint) (setup.Draft, error) {
	draft := setup.NewDraft(cfg, defaults)
	endpoints := make(map[string]topology.Endpoint, len(cfg.Topology.Endpoints))
	for _, endpoint := range cfg.Topology.Endpoints {
		endpoints[endpoint.ID] = endpoint
	}
	assignments := []topology.Assignment{cfg.Topology.Roles.King, cfg.Topology.Roles.Worker}
	if cfg.CouncilEnabled {
		assignments = append(assignments, cfg.Topology.Roles.Council)
	}
	seen := make(map[setup.ModelRef]bool)
	options := make([]setup.ModelOption, 0, len(assignments))
	for _, assignment := range assignments {
		ref := setup.ModelRef{EndpointID: assignment.EndpointID, ModelID: strings.TrimSpace(assignment.Model)}
		if !ref.Assignment().Complete() || seen[ref] {
			continue
		}
		endpoint, ok := endpoints[ref.EndpointID]
		if !ok {
			return setup.Draft{}, fmt.Errorf("configured endpoint %q is unavailable", ref.EndpointID)
		}
		seen[ref] = true
		options = append(options, setup.ModelOption{Ref: ref, Endpoint: endpoint, Installed: true})
	}
	draft.ReplaceCatalog(options)
	if len(draft.SelectedModels()) == 0 {
		return setup.Draft{}, fmt.Errorf("no configured models are available for the Wizard")
	}
	return draft, nil
}

func (m *Model) startWizard() {
	previous := append([]topology.Endpoint(nil), m.config.Topology.Endpoints...)
	save := m.save
	session := wizard.NewSessionWithSave(&m.workflow.Draft, func(next config.Config) error {
		next.Topology.Endpoints = m.workflow.Draft.PersistenceEndpoints(previous)
		if save == nil {
			return nil
		}
		return save(next)
	})
	m.wizardSession = session
	m.wizardEngine = wizard.NewEngine(m.wizardClient, m.wizardModel, session)
	m.wizardInput = ui.NewChatInput()
	m.wizardMessages = nil
	m.wizardReady = false
	m.wizardBusy = false
	m.wizardApplying = false
}

func (m Model) handleWizardKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	if key == "tab" {
		if m.wizardBusy || m.wizardApplying {
			m.workflow.Err = fmt.Errorf("wait for the current Wizard action to finish before opening Manual setup")
			return m, nil
		}
		return m.openManualSetup(), nil
	}
	if key == "esc" {
		if m.wizardCancel != nil {
			m.wizardCancel()
		}
		m.wizardGeneration++
		m.cancelWizardWarmup()
		if m.wizardReturnToReady {
			m.setup = false
			m.workflow.State = setup.StateReady
			m.screen = setup.StateReady
		} else {
			m.workflow.Back()
			m.screen = m.workflow.State
		}
		m.wizardBusy = false
		m.wizardApplying = false
		m.wizardPreparing = false
		return m, nil
	}
	if m.wizardBusy || m.wizardApplying {
		return m, nil
	}
	if key == "ctrl+enter" {
		message := strings.TrimSpace(m.wizardInput.Value())
		if message == "" {
			return m, nil
		}
		if m.wizardEngine == nil {
			m.workflow.Err = fmt.Errorf("the local Wizard model is still starting; you can apply the defaults now or retry in a moment")
			return m, nil
		}
		m.wizardInput.SetValue("")
		m.wizardMessages = append(m.wizardMessages, "You: "+message)
		m.wizardBusy = true
		engine := m.wizardEngine
		generation := m.wizardGeneration
		return m, func() tea.Msg {
			reply, err := engine.Respond(context.Background(), message)
			return wizardReplyMsg{generation: generation, reply: reply, err: err}
		}
	}
	if key == "enter" && m.wizardReady && strings.TrimSpace(m.wizardInput.Value()) == "" {
		return m.beginWizardApply()
	}
	var command tea.Cmd
	m.wizardInput, command = m.wizardInput.Update(msg)
	return m, command
}

func (m Model) openManualSetup() Model {
	if m.wizardCancel != nil {
		m.wizardCancel()
		m.wizardCancel = nil
	}
	m.wizardGeneration++
	m.cancelWizardWarmup()
	m.wizardBusy = false
	m.wizardApplying = false
	m.wizardPreparing = false
	m.wizardManual = true
	m.workflow.Err = nil
	m.workflow.State = setup.StateRoles
	m.screen = setup.StateRoles
	m.role = 0
	m.modelIndex = 0
	king := m.workflow.Draft.Config.Topology.Roles.King
	for index, option := range m.workflow.Draft.SelectedModels() {
		if option.Ref.Assignment() == king {
			m.modelIndex = index
			break
		}
	}
	return m
}

func (m *Model) cancelWizardWarmup() {
	if m.wizardWarmCancel != nil {
		m.wizardWarmCancel()
		m.wizardWarmCancel = nil
	}
	m.wizardWarmGeneration++
	m.wizardWarming = false
	m.runtimeWarm = nil
	m.runtimeWarmSignature = ""
}

func (m Model) returnToWizardFromManual() (Model, tea.Cmd) {
	returnToReady := m.wizardReturnToReady
	m.wizardManual = false
	m.workflow.State = setup.StateWizard
	m.screen = setup.StateWizard
	m, command := m.beginImmediateWizard(returnToReady)
	m.wizardMessages = []string{"Wizard: Your manual changes are reflected in the Proposed Kingdom below."}
	return m, command
}

func (m Model) beginWizardApply() (tea.Model, tea.Cmd) {
	if m.wizardSession == nil {
		m.workflow.Err = fmt.Errorf("Wizard setup session is unavailable")
		return m, nil
	}
	m.wizardApplying = true
	m.wizardSession.AuthorizeApply()
	session := m.wizardSession
	generation := m.wizardGeneration
	return m, func() tea.Msg {
		result := session.Run(context.Background(), tools.Call{ID: "wizard-apply", Name: "apply_setup", Arguments: json.RawMessage(`{}`)})
		if result.Error != "" {
			return wizardApplyMsg{generation: generation, err: fmt.Errorf("%s", result.Error)}
		}
		applied, ok := session.AppliedConfig()
		if !ok {
			return wizardApplyMsg{generation: generation, err: fmt.Errorf("Wizard did not return applied configuration")}
		}
		return wizardApplyMsg{generation: generation, config: applied}
	}
}

func wizardModelLabel(model setup.ModelOption) string {
	if !model.Ref.Assignment().Complete() {
		return ""
	}
	return model.Ref.ModelID + " · fast setup model"
}
