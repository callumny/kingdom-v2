package app

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestModelsScreenSelectsAcrossProviders(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(crossProviderResults())

	m, _ = update(m, key("enter")) // welcome -> providers
	m, _ = update(m, key("enter")) // providers -> models
	if m.screen != setup.StateModels {
		t.Fatalf("screen=%v, want models", m.screen)
	}
	m, _ = update(m, key(" "))
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	selected := m.workflow.Draft.SelectedModels()
	if len(selected) != 2 || selected[0].Ref.EndpointID != "ollama-local" || selected[1].Ref.EndpointID != "mlx-local" {
		t.Fatalf("selected=%+v", selected)
	}
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateRoles {
		t.Fatalf("screen=%v, want roles", m.screen)
	}
}

func TestProviderScreenDoesNotFilterModelProviders(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(onboardingResults())
	m, _ = update(m, key("enter"))
	view := m.View().Content
	if strings.Contains(view, "[✓]") || strings.Contains(view, "Space Toggle") {
		t.Fatalf("provider screen still exposes provider selection: %s", view)
	}
	m, _ = update(m, key("enter"))
	view = m.View().Content
	for _, want := range []string{"ollama-model", "lm-model-a", "lm-model-b"} {
		if !strings.Contains(view, want) {
			t.Fatalf("models view missing %q: %s", want, view)
		}
	}
}

func crossProviderResults() []setup.EndpointResult {
	return []setup.EndpointResult{
		{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: []discovery.Model{{ID: "ollama-small", ParameterSize: "3B"}}},
		{Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}, Models: []discovery.Model{{ID: "mlx-large", ParameterSize: "14B"}}},
	}
}
