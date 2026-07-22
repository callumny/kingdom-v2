package app

import (
	"context"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/wizard"
)

type appWizardClient struct {
	responses []string
	calls     int
}

func (f *appWizardClient) Chat(context.Context, topology.Endpoint, string, []modelapi.Message) (string, error) {
	f.calls++
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestModelsOpenWizardImmediatelyWithTheSmallestSelectedModel(t *testing.T) {
	chat := &appWizardClient{}
	var prepared setup.ModelOption
	m := wizardAppModel(func(_ context.Context, _ config.Config, model setup.ModelOption) (setup.ModelOption, error) {
		prepared = model
		model.Endpoint.BaseURL = "http://127.0.0.1:18083"
		return model, nil
	}, chat, nil)
	m.screen, m.workflow.State = setup.StateModels, setup.StateModels

	m, command := m.advanceFromModels()
	if command == nil || m.screen != setup.StateWizard || !m.wizardReady || m.wizardModel.Ref.ModelID != "small" {
		t.Fatalf("Wizard did not open immediately: screen=%v model=%+v ready=%v command=%v", m.screen, m.wizardModel, m.wizardReady, command)
	}
	if chat.calls != 0 || len(m.wizardMessages) == 0 {
		t.Fatalf("opening Wizard made model calls=%d messages=%v", chat.calls, m.wizardMessages)
	}
	if prepared.Ref.ModelID != "" {
		t.Fatalf("runtime preparation blocked Wizard entry: %+v", prepared)
	}
	m, _ = update(m, command())
	if prepared.Ref.ModelID != "small" || m.wizardEngine == nil || m.wizardModel.Endpoint.BaseURL != "http://127.0.0.1:18083" {
		t.Fatalf("prepared=%+v Wizard model=%+v engine=%v", prepared, m.wizardModel, m.wizardEngine)
	}
}

func TestWizardPreparationFailureDoesNotBlockApplyingDefaults(t *testing.T) {
	var saved config.Config
	m := wizardAppModel(func(_ context.Context, _ config.Config, model setup.ModelOption) (setup.ModelOption, error) {
		return model, context.DeadlineExceeded
	}, &appWizardClient{}, func(next config.Config) error {
		saved = next
		return nil
	})
	m.screen, m.workflow.State = setup.StateModels, setup.StateModels
	m, prepare := m.advanceFromModels()
	m, _ = update(m, prepare())
	if m.screen != setup.StateWizard || !m.wizardReady || m.workflow.Err == nil {
		t.Fatalf("preparation failure blocked Wizard: screen=%v ready=%v err=%v", m.screen, m.wizardReady, m.workflow.Err)
	}
	m, apply := update(m, key("enter"))
	if apply == nil {
		t.Fatal("defaults could not be applied after preparation failure")
	}
	m, _ = update(m, apply())
	if m.setup || m.screen != setup.StateReady || saved.Topology.Roles.Worker.Model != "small" {
		t.Fatalf("defaults not saved: setup=%v screen=%v saved=%+v", m.setup, m.screen, saved)
	}
}

func TestWizardCanChangeDraftAndApplyIntoNormalChat(t *testing.T) {
	var saved config.Config
	chat := &appWizardClient{responses: []string{
		`{"type":"tool","name":"enable_council","arguments":{"enabled":false}}`,
		`{"type":"message","content":"Council disabled.","ready":true}`,
	}}
	m := wizardAppModel(nil, chat, func(next config.Config) error { saved = next; return nil })
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

func TestSlashWizardReopensConfiguredSetupWithoutRunningAPrompt(t *testing.T) {
	cfg := completeConfig()
	chat := &appWizardClient{}
	runs := 0
	m := NewWithServices(cfg, Services{
		Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event {
			runs++
			return nil
		},
		PrepareWizard: func(_ context.Context, _ config.Config, model setup.ModelOption) (setup.ModelOption, error) {
			return model, nil
		},
		WizardClient: chat,
	})
	m.chat.SetValue("/wizard")
	m, command := update(m, key("ctrl+enter"))
	if command == nil || !m.setup || m.screen != setup.StateWizard || !m.wizardReturnToReady || m.wizardModel.Ref != (setup.ModelRef{EndpointID: "local", ModelID: "w"}) {
		t.Fatalf("slash Wizard: setup=%v screen=%v return=%v model=%+v command=%v", m.setup, m.screen, m.wizardReturnToReady, m.wizardModel, command)
	}
	if runs != 0 || m.chat.Value() != "" {
		t.Fatalf("slash command reached orchestration: runs=%d input=%q", runs, m.chat.Value())
	}
	m, _ = update(m, key("esc"))
	if m.setup || m.screen != setup.StateReady {
		t.Fatalf("Esc did not return to chat: setup=%v screen=%v", m.setup, m.screen)
	}
}

func TestWizardTabOpensManualSetupAndEscapeReturns(t *testing.T) {
	m := wizardAppModel(nil, &appWizardClient{}, nil)
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	m.screen, m.workflow.State = setup.StateWizard, setup.StateWizard
	m.startWizard()

	m, command := update(m, key("tab"))
	if command != nil || !m.wizardManual || m.screen != setup.StateRoles || m.workflow.State != setup.StateRoles {
		t.Fatalf("manual setup did not open: manual=%v screen=%v state=%v command=%v", m.wizardManual, m.screen, m.workflow.State, command)
	}
	for _, want := range []string{"Manual setup", "assign models to roles", "Swap King/Worker"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("manual view missing %q: %s", want, m.View().Content)
		}
	}

	m, command = update(m, key("esc"))
	if m.wizardManual || m.screen != setup.StateWizard || m.workflow.State != setup.StateWizard || !strings.Contains(strings.Join(m.wizardMessages, " "), "manual changes") {
		t.Fatalf("manual setup did not return to Wizard: manual=%v screen=%v state=%v messages=%v command=%v", m.wizardManual, m.screen, m.workflow.State, m.wizardMessages, command)
	}
}

func TestManualSetupCanSwapKingAndWorkerWithoutTheModel(t *testing.T) {
	m := wizardAppModel(nil, &appWizardClient{}, nil)
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	before := m.workflow.Draft.Config.Topology.Roles
	m.screen, m.workflow.State = setup.StateWizard, setup.StateWizard
	m.startWizard()
	m, _ = update(m, key("tab"))
	m, _ = update(m, key("x"))
	after := m.workflow.Draft.Config.Topology.Roles
	if after.King != before.Worker || after.Worker != before.King {
		t.Fatalf("manual swap failed: before=%+v after=%+v", before, after)
	}
}

func TestManualSetupValidatesAndSavesWithoutTheWizardModel(t *testing.T) {
	var saved config.Config
	m := wizardAppModel(nil, &appWizardClient{}, func(next config.Config) error {
		saved = next
		return nil
	})
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	m.screen, m.workflow.State = setup.StateWizard, setup.StateWizard
	m.startWizard()
	m, _ = update(m, key("tab"))
	m, _ = update(m, key("x"))
	m, _ = update(m, key("n"))
	m, _ = update(m, key("down"))
	m, _ = update(m, key("right"))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateReview {
		t.Fatalf("manual setup did not reach review: %v", m.screen)
	}
	m, command := update(m, key("enter"))
	if command == nil {
		t.Fatal("manual setup did not start save")
	}
	m, _ = update(m, command())
	if m.setup || m.screen != setup.StateReady || saved.WorkerConcurrency != 5 || saved.Topology.Roles.King.Model != "small" || saved.Topology.Roles.Worker.Model != "large" {
		t.Fatalf("manual setup not saved: setup=%v screen=%v saved=%+v", m.setup, m.screen, saved)
	}
}

func wizardAppModel(prepare WizardPrepareFunc, chat modelapi.ChatClient, save func(config.Config) error) Model {
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}}
	m := NewWithServices(cfg, Services{Save: save, PrepareWizard: prepare, WizardClient: chat})
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
