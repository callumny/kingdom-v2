package app

import (
	"context"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/wizard"
)

type appCompletionClient struct {
	responses []modelapi.Completion
}

func (f *appCompletionClient) Complete(context.Context, topology.Endpoint, string, []modelapi.Message, int) (modelapi.Completion, error) {
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

type appWizardClient struct {
	responses []string
}

func (f *appWizardClient) Chat(context.Context, topology.Endpoint, string, []modelapi.Message) (string, error) {
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestModelsAdvanceThroughBenchmarkIntoWizard(t *testing.T) {
	completion := &appCompletionClient{responses: []modelapi.Completion{
		{Content: "ready"},
		{Content: `{"tool":{"name":"inspect_setup","arguments":{}}}`, CompletionTokens: 20, GenerationDuration: time.Second},
		{Content: "ready"},
		{Content: `{"tool":{"name":"inspect_setup","arguments":{}}}`, CompletionTokens: 40, GenerationDuration: time.Second},
	}}
	chat := &appWizardClient{responses: []string{`{"type":"message","content":"I prepared your setup.","ready":true}`}}
	m := wizardAppModel(wizard.Benchmarker{Client: completion, TimeoutPerModel: time.Second}, chat, nil)
	m.screen, m.workflow.State = setup.StateModels, setup.StateModels

	var cmd any
	m, command := m.advanceFromModels()
	if command == nil || m.screen != setup.StateBenchmark {
		t.Fatalf("benchmark did not start: screen=%v cmd=%v", m.screen, command)
	}
	for steps := 0; steps < 12 && !m.wizardReady; steps++ {
		if command == nil {
			t.Fatal("Wizard command sequence ended before it became ready")
		}
		message := command()
		m, command = update(m, message)
		cmd = command
	}
	if m.screen != setup.StateWizard || m.wizardModel.Ref.ModelID != "small" || len(m.wizardMessages) == 0 || !m.wizardReady {
		t.Fatalf("Wizard state: screen=%v model=%+v messages=%v ready=%v cmd=%v", m.screen, m.wizardModel, m.wizardMessages, m.wizardReady, cmd)
	}
}

func TestWizardCanChangeDraftAndApplyIntoNormalChat(t *testing.T) {
	var saved config.Config
	chat := &appWizardClient{responses: []string{
		`{"type":"tool","name":"enable_council","arguments":{"enabled":false}}`,
		`{"type":"message","content":"Council disabled.","ready":true}`,
	}}
	m := wizardAppModel(wizard.Benchmarker{}, chat, func(next config.Config) error { saved = next; return nil })
	m.wizardModel = m.workflow.Draft.SelectedModels()[0]
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	m.startWizard()
	m.screen, m.workflow.State = setup.StateWizard, setup.StateWizard
	m.wizardBusy = true
	m, _ = update(m, wizardReplyMsg{generation: m.wizardGeneration, reply: wizard.Reply{Content: "Setup ready.", Ready: true}})
	m.wizardInput.SetValue("Disable the council")
	m, command := update(m, key("ctrl+enter"))
	for steps := 0; steps < 4 && m.wizardBusy; steps++ {
		m, command = update(m, command())
	}
	if m.workflow.Draft.Config.CouncilEnabled || !m.wizardReady {
		t.Fatalf("change not applied: %+v", m.workflow.Draft.Config)
	}
	m, command = update(m, key("enter"))
	if command == nil || !m.wizardApplying {
		t.Fatal("apply did not start")
	}
	m, _ = update(m, command())
	if m.setup || m.screen != setup.StateReady || saved.Topology.Roles.King.Model == "" {
		t.Fatalf("apply failed: setup=%v screen=%v saved=%+v", m.setup, m.screen, saved)
	}
}

func wizardAppModel(benchmarker wizard.Benchmarker, chat modelapi.ChatClient, save func(config.Config) error) Model {
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}}
	m := NewWithServices(cfg, Services{Save: save, WizardBenchmark: benchmarker, WizardClient: chat})
	options := []setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "large"}, Endpoint: cfg.Topology.Endpoints[0], Installed: true, ParameterSize: "14B"},
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "small"}, Endpoint: cfg.Topology.Endpoints[0], Installed: true, ParameterSize: "3B"},
	}
	m.workflow.Draft.ReplaceCatalog(options)
	for _, option := range options {
		_ = m.workflow.Draft.ToggleModel(option.Ref)
	}
	return m
}
