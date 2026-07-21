package app

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestModelsScreenLoadsInstalledInventoryAcrossEnabledProviders(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{
		{
			Kind:      localmodels.KindOllama,
			Name:      "Ollama",
			Installed: true,
			Running:   true,
			Models:    []localmodels.Model{{ID: "qwen3:8b", Loaded: true}},
			Endpoint:  topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"},
		},
		{
			Kind:      localmodels.KindMLX,
			Name:      "MLX",
			Installed: true,
			Models:    []localmodels.Model{{ID: "mlx-community/Qwen3-4B-4bit"}, {ID: "mlx-community/Mistral-7B-4bit"}},
			Endpoint:  topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"},
		},
	}}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: discovery.DefaultEndpoints()[0]},
		{Endpoint: discovery.DefaultEndpoints()[1]},
	})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.Config.Providers.MLX.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m.workflow.Draft.SetProviderReady(setup.MLXEndpointID, true)

	m, command := update(m, key("enter"))
	if m.screen != setup.StateModels || command == nil {
		t.Fatalf("models inventory did not start: screen=%v command=%v", m.screen, command)
	}
	if view := m.View().Content; !strings.Contains(view, "Checking installed models") {
		t.Fatalf("missing inventory loading state: %s", view)
	}

	m, _ = update(m, command())
	view := m.View().Content
	for _, want := range []string{"qwen3:8b", "mlx-community/Qwen3-4B-4bit", "mlx-community/Mistral-7B-4bit", "Ollama", "MLX"} {
		if !strings.Contains(view, want) {
			t.Fatalf("combined inventory missing %q: %s", want, view)
		}
	}
}

func TestModelsScreenSelectsAcrossProviders(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(crossProviderResults())

	_ = m.workflow.Draft.SetProviderEnabled(setup.OllamaEndpointID, true, setup.Platform{OS: "linux", Arch: "amd64"})
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

func TestProviderScreenRequiresAnExplicitProviderChoice(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(onboardingResults())
	m, _ = update(m, key("enter"))
	view := m.View().Content
	if m.screen != setup.StateProviders || !strings.Contains(view, "enable at least one provider") {
		t.Fatalf("provider screen advanced without a choice: %s", view)
	}
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	view = m.View().Content
	for _, want := range []string{"ollama-model", "mlx-model-a", "mlx-model-b"} {
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
