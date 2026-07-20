package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestWelcomeScansThenOpensProviders(t *testing.T) {
	discover := func(_ context.Context, generation uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg {
			return DiscoveryMsg{Generation: generation, Results: onboardingResults()}
		}
	}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Discover: discover})
	if m.screen != setup.StateWelcome || !strings.Contains(m.View().Content, "Welcome to Kingdom") {
		t.Fatalf("initial onboarding screen=%v view=%s", m.screen, m.View().Content)
	}
	command := m.Init()
	if command == nil {
		t.Fatal("welcome did not start model discovery")
	}
	m, _ = update(m, command())
	if m.screen != setup.StateWelcome {
		t.Fatalf("discovery skipped welcome: %v", m.screen)
	}
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateProviders {
		t.Fatalf("enter opened %v, want providers", m.screen)
	}
	for _, want := range []string{"Choose your model providers", "Ollama", "LM Studio", "2 models"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("provider screen missing %q: %s", want, m.View().Content)
		}
	}
}

func TestProviderSelectionFiltersModelsForNextStep(t *testing.T) {
	discover := func(_ context.Context, generation uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg { return DiscoveryMsg{Generation: generation, Results: onboardingResults()} }
	}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), Discover: discover})
	m, _ = update(m, m.Init()())
	m, _ = update(m, key("enter"))
	m, _ = update(m, key(" ")) // deselect Ollama
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateRoles {
		t.Fatalf("provider continue opened %v", m.screen)
	}
	view := m.View().Content
	if strings.Contains(view, "ollama-model") || !strings.Contains(view, "lm-model") {
		t.Fatalf("provider selection was not applied to roles: %s", view)
	}
}

func onboardingResults() []setup.EndpointResult {
	endpoints := discovery.DefaultEndpoints()
	return []setup.EndpointResult{
		{Endpoint: endpoints[0], Models: []discovery.Model{{ID: "ollama-model"}}},
		{Endpoint: endpoints[1], Models: []discovery.Model{{ID: "lm-model-a"}, {ID: "lm-model-b"}}},
		{Endpoint: endpoints[2], Err: context.DeadlineExceeded},
	}
}
