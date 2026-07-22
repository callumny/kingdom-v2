package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
	"github.com/callumny/kingdom/internal/wizard"
)

type wizardBenchmarkEvent struct {
	progress *wizard.BenchmarkProgress
	results  []wizard.BenchmarkResult
	done     bool
}

type wizardBenchmarkMsg struct {
	generation uint64
	event      wizardBenchmarkEvent
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

func (m Model) beginWizardBenchmark() (Model, tea.Cmd) {
	if m.wizardCancel != nil {
		m.wizardCancel()
	}
	m.wizardGeneration++
	generation := m.wizardGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.wizardCancel = cancel
	m.wizardBenchmarkActive = true
	m.wizardBenchmarkResults = nil
	m.wizardMessages = nil
	m.wizardReady = false
	channel := make(chan wizardBenchmarkEvent, 8)
	m.wizardBenchmarkCh = channel
	models := m.workflow.Draft.SelectedModels()
	runner := m.wizardBenchmarker
	if runner.Client == nil {
		m.wizardBenchmarkActive = false
		m.wizardBenchmarkCh = nil
		m.workflow.Err = fmt.Errorf("Wizard benchmark client is unavailable")
		return m, nil
	}
	go func() {
		defer close(channel)
		emit := func(event wizardBenchmarkEvent) bool {
			select {
			case channel <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		results := runner.Run(ctx, models, func(progress wizard.BenchmarkProgress) {
			value := progress
			emit(wizardBenchmarkEvent{progress: &value})
		})
		emit(wizardBenchmarkEvent{results: results, done: true})
	}()
	return m, m.nextWizardBenchmarkEvent(generation)
}

func (m Model) nextWizardBenchmarkEvent(generation uint64) tea.Cmd {
	channel := m.wizardBenchmarkCh
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			event = wizardBenchmarkEvent{done: true}
		}
		return wizardBenchmarkMsg{generation: generation, event: event}
	}
}

func (m Model) finishWizardBenchmark() (Model, tea.Cmd) {
	var failures []string
	for _, result := range m.wizardBenchmarkResults {
		if result.Error != "" {
			failures = append(failures, result.Model.Ref.ModelID+": "+result.Error)
		}
	}
	if len(failures) > 0 {
		m.workflow.Err = fmt.Errorf("selected model could not be tested: %s", strings.Join(failures, "; "))
		return m, nil
	}
	winner, ok := wizard.FastestReliable(m.wizardBenchmarkResults)
	if ok {
		m.wizardModel = winner.Model
	} else {
		if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
			m.workflow.Err = err
			return m, nil
		}
		worker := m.workflow.Draft.Config.Topology.Roles.Worker
		for _, result := range m.wizardBenchmarkResults {
			if result.Model.Ref.Assignment() == worker {
				m.wizardModel = result.Model
				break
			}
		}
	}
	if !m.wizardModel.Ref.Assignment().Complete() {
		m.workflow.Err = fmt.Errorf("no selected model can run the Wizard")
		return m, nil
	}
	if m.wizardClient == nil {
		m.workflow.Err = fmt.Errorf("Wizard model client is unavailable")
		return m, nil
	}
	if err := m.workflow.Continue(); err != nil {
		m.workflow.Err = err
		return m, nil
	}
	m.screen = m.workflow.State
	m.startWizard()
	m.wizardBusy = true
	engine := m.wizardEngine
	generation := m.wizardGeneration
	return m, func() tea.Msg {
		reply, err := engine.Start(context.Background())
		return wizardReplyMsg{generation: generation, reply: reply, err: err}
	}
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
	if key == "esc" {
		if m.wizardCancel != nil {
			m.wizardCancel()
		}
		m.wizardGeneration++
		m.workflow.Back()
		m.screen = m.workflow.State
		m.wizardBusy = false
		m.wizardApplying = false
		return m, nil
	}
	if m.wizardBusy || m.wizardApplying {
		return m, nil
	}
	if key == "ctrl+enter" {
		message := strings.TrimSpace(m.wizardInput.Value())
		if message == "" || m.wizardEngine == nil {
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

func wizardBenchmarkRows(results []wizard.BenchmarkResult) []ui.WizardBenchmarkRow {
	rows := make([]ui.WizardBenchmarkRow, 0, len(results))
	for _, result := range results {
		provider := result.Model.Endpoint.Name
		if provider == "" {
			provider = result.Model.Ref.EndpointID
		}
		rows = append(rows, ui.WizardBenchmarkRow{Provider: provider, Model: result.Model.Ref.ModelID, Status: result.Label()})
	}
	return rows
}

func wizardModelLabel(model setup.ModelOption, results []wizard.BenchmarkResult) string {
	if !model.Ref.Assignment().Complete() {
		return ""
	}
	for _, result := range results {
		if result.Model.Ref == model.Ref && result.Reliable {
			return fmt.Sprintf("%s · %.1f tok/s", model.Ref.ModelID, result.TokensPerSecond)
		}
	}
	return model.Ref.ModelID + " · fallback"
}
