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

func (f *fakeProviderInstaller) Install(_ context.Context, kind localmodels.Kind, _, _ string) error {
	f.calls = append(f.calls, kind)
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
	m.workflow.Draft.ApplyResults(onboardingResults())

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
	m, _ = update(m, command())
	if m.providerInstalling || len(installer.calls) != 1 || installer.calls[0] != localmodels.KindOllama || !m.workflow.Draft.Config.Providers.Ollama.Enabled {
		t.Fatalf("installation result: installing=%v calls=%v config=%+v", m.providerInstalling, installer.calls, m.workflow.Draft.Config.Providers)
	}
}
