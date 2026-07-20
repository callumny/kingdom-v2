package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type fakeProviderInstaller struct {
	calls []localmodels.Kind
	err   error
}

func (f *fakeProviderInstaller) InstallWithProgress(_ context.Context, kind localmodels.Kind, _, _ string, report localmodels.ProgressReporter) error {
	f.calls = append(f.calls, kind)
	report(localmodels.InstallProgress{Completed: 1, Total: 2, Message: "Preparing provider"})
	report(localmodels.InstallProgress{Completed: 2, Total: 2, Message: "Provider installed"})
	return f.err
}

func TestOnboardingStartsWithProvidersAndScans(t *testing.T) {
	discover := func(_ context.Context, generation uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg {
			return DiscoveryMsg{Generation: generation, Results: onboardingResults()}
		}
	}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Discover: discover})
	if m.screen != setup.StateProviders || !strings.Contains(m.View().Content, "Set up model providers") {
		t.Fatalf("initial onboarding screen=%v view=%s", m.screen, m.View().Content)
	}
	command := m.Init()
	if command == nil {
		t.Fatal("providers did not start model discovery")
	}
	m, _ = update(m, command())
	if m.screen != setup.StateProviders {
		t.Fatalf("discovery changed screen to %v", m.screen)
	}
	for _, want := range []string{"Set up model providers", "Ollama", "MLX", "1 model"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("provider screen missing %q: %s", want, m.View().Content)
		}
	}
}

func TestProvidersLeadToCombinedModelCatalog(t *testing.T) {
	discover := func(_ context.Context, generation uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg { return DiscoveryMsg{Generation: generation, Results: onboardingResults()} }
	}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Discover: discover})
	m, _ = update(m, m.Init()())
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateModels {
		t.Fatalf("provider continue opened %v", m.screen)
	}
	view := m.View().Content
	if !strings.Contains(view, "ollama-model") || !strings.Contains(view, "mlx-model-a") {
		t.Fatalf("combined model catalogue missing providers: %s", view)
	}
}

func onboardingResults() []setup.EndpointResult {
	endpoints := discovery.DefaultEndpoints()
	return []setup.EndpointResult{
		{Endpoint: endpoints[0], Models: []discovery.Model{{ID: "ollama-model"}}},
		{Endpoint: endpoints[1], Models: []discovery.Model{{ID: "mlx-model-a"}, {ID: "mlx-model-b"}}},
	}
}

func TestProviderInstallRequiresExplicitConfirmation(t *testing.T) {
	installer := &fakeProviderInstaller{}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Installer: installer})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: discovery.DefaultEndpoints()[0], Err: context.DeadlineExceeded},
		{Endpoint: discovery.DefaultEndpoints()[1], Err: context.DeadlineExceeded},
	})

	m, command := update(m, key("i"))
	if command != nil || !m.providerConfirming || len(installer.calls) != 0 {
		t.Fatalf("install was not gated: confirming=%v calls=%v", m.providerConfirming, installer.calls)
	}
	m, command = update(m, key("n"))
	if command != nil || m.providerConfirming || len(installer.calls) != 0 {
		t.Fatalf("cancel reached installer: confirming=%v calls=%v", m.providerConfirming, installer.calls)
	}

	m, _ = update(m, key("i"))
	m, command = update(m, key("y"))
	if command == nil || !m.providerInstalling {
		t.Fatal("confirmation did not start installation")
	}
	sawProgress := false
	for command != nil {
		m, command = update(m, command())
		if strings.Contains(m.View().Content, "50%") && strings.Contains(m.View().Content, "Preparing provider") {
			sawProgress = true
		}
	}
	if !sawProgress {
		t.Fatalf("installation progress was not rendered: %s", m.View().Content)
	}
	if m.providerInstalling || len(installer.calls) != 1 || installer.calls[0] != localmodels.KindOllama || !m.workflow.Draft.Config.Providers.Ollama.Enabled {
		t.Fatalf("installation result: installing=%v calls=%v config=%+v", m.providerInstalling, installer.calls, m.workflow.Draft.Config.Providers)
	}
}

func TestProvidersCannotContinueUntilEveryEnabledProviderIsReady(t *testing.T) {
	m := NewWithServices(config.Default(), Services{})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0], Err: context.DeadlineExceeded}})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateProviders || m.workflow.Err == nil || !strings.Contains(m.workflow.Err.Error(), "Ollama") {
		t.Fatalf("unready provider advanced: screen=%v err=%v", m.screen, m.workflow.Err)
	}
}

func TestInstalledMLXIsReadyWithoutRunningAModelServer(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{{Kind: localmodels.KindMLX, Name: "MLX", Installed: true}}}
	discover := func(_ context.Context, generation uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg {
			return DiscoveryMsg{Generation: generation, Results: []setup.EndpointResult{
				{Endpoint: discovery.DefaultEndpoints()[0], Err: context.DeadlineExceeded},
				{Endpoint: discovery.DefaultEndpoints()[1], Err: context.DeadlineExceeded},
			}}
		}
	}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Discover: discover, LocalModels: manager})
	m, inspect := update(m, m.Init()())
	if inspect == nil {
		t.Fatal("provider runtime inspection was not scheduled")
	}
	m, _ = update(m, inspect())
	if !m.workflow.Draft.ProviderReady(setup.MLXEndpointID) {
		t.Fatal("installed MLX runtime was not marked ready")
	}
	m.workflow.Draft.Config.Providers.MLX.Enabled = true
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateModels {
		t.Fatalf("installed MLX did not advance: screen=%v err=%v", m.screen, m.workflow.Err)
	}
}
