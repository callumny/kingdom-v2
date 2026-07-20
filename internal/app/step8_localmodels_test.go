package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type fakeLocalModelManager struct {
	runtimes []localmodels.Runtime
	err      error
	starts   []struct {
		kind  localmodels.Kind
		model string
	}
}

func (m *fakeLocalModelManager) Inspect(context.Context) []localmodels.Runtime {
	return append([]localmodels.Runtime(nil), m.runtimes...)
}

func (m *fakeLocalModelManager) StartAndWait(_ context.Context, kind localmodels.Kind, model string) error {
	m.starts = append(m.starts, struct {
		kind  localmodels.Kind
		model string
	}{kind: kind, model: model})
	return m.err
}

func runtimeFixtures() []localmodels.Runtime {
	return []localmodels.Runtime{
		{Kind: localmodels.KindOllama, Name: "Ollama", Installed: true, Running: false, Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://localhost:11434"}},
		{Kind: localmodels.KindLMStudio, Name: "LM Studio", Installed: true, Running: true, Models: []localmodels.Model{{ID: "alpha", Loaded: true}, {ID: "beta"}}, Endpoint: topology.Endpoint{ID: "lm-studio-local", Name: "LM Studio", Kind: topology.KindOpenAICompatible, BaseURL: "http://localhost:1234/v1"}},
		{Kind: localmodels.KindMLX, Name: "MLX", Installed: false, Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://localhost:8080/v1"}},
	}
}

func TestControlROpensLocalModelsAndKeepsKeysOutOfChat(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager})
	m, command := update(m, key("ctrl+r"))
	if !m.localModels.open || command == nil || !strings.Contains(m.View().Content, "Inspecting local runtimes") {
		t.Fatalf("local models did not start loading: %s", m.View().Content)
	}
	m, _ = update(m, command())
	view := m.View().Content
	for _, expected := range []string{"Local Models", "Ollama", "LM Studio", "MLX", "running", "not installed"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q: %s", expected, view)
		}
	}
	m, _ = update(m, key("right"))
	if view = m.View().Content; !strings.Contains(view, "alpha") || !strings.Contains(view, "beta") {
		t.Fatalf("selected runtime models missing: %s", view)
	}
	m.chat.SetValue("unchanged")
	m, _ = update(m, key("x"))
	if m.chat.Value() != "unchanged" {
		t.Fatal("local model browser leaked input into chat")
	}
	m, _ = update(m, key("esc"))
	if m.localModels.open {
		t.Fatal("escape did not close local model browser")
	}
}

func TestDiscoveryGuidesEmptySetupIntoLocalModels(t *testing.T) {
	for _, pressed := range []string{"enter", "m"} {
		t.Run(pressed, func(t *testing.T) {
			manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
			m := NewWithServices(config.Default(), Services{LocalModels: manager})
			m.workflow.Draft.ApplyResults([]setup.EndpointResult{{
				Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
				Err:      errors.New("connection refused"),
			}})

			m, _ = update(m, key("enter")) // welcome -> providers
			m, inspect := update(m, key(pressed))
			if !m.localModels.open || inspect == nil {
				t.Fatalf("%s did not open local model setup", pressed)
			}
			m, _ = update(m, inspect())
			if len(m.localModels.runtimes) != len(runtimeFixtures()) {
				t.Fatalf("runtime inspection missing: %+v", m.localModels.runtimes)
			}
		})
	}
}

func TestLocalModelStartRequiresConfirmationAndRefreshes(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager})
	m, inspect := update(m, key("ctrl+r"))
	m, _ = update(m, inspect())

	m, _ = update(m, key("s"))
	if !m.localModels.confirming || len(manager.starts) != 0 || !strings.Contains(m.View().Content, "Start Ollama?") {
		t.Fatal("Ollama start was not gated")
	}
	m, _ = update(m, key("n"))
	if len(manager.starts) != 0 {
		t.Fatal("cancelled start reached runtime manager")
	}
	m, _ = update(m, key("s"))
	m, start := update(m, key("y"))
	if start == nil || !m.localModels.starting {
		t.Fatal("confirmed start did not run asynchronously")
	}
	m, refresh := update(m, start())
	if len(manager.starts) != 1 || manager.starts[0].kind != localmodels.KindOllama || manager.starts[0].model != "" || refresh == nil {
		t.Fatalf("starts=%+v", manager.starts)
	}
	m, _ = update(m, refresh())
	if m.localModels.starting || m.localModels.loading {
		t.Fatal("runtime refresh did not settle")
	}
}

func TestLocalModelNavigationLoadsSelectedLMStudioModel(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager})
	m, inspect := update(m, key("ctrl+r"))
	m, _ = update(m, inspect())
	m, _ = update(m, key("right"))
	m, _ = update(m, key("down"))
	m, _ = update(m, key("s"))
	m, start := update(m, key("y"))
	m, _ = update(m, start())
	if len(manager.starts) != 1 || manager.starts[0].kind != localmodels.KindLMStudio || manager.starts[0].model != "beta" {
		t.Fatalf("starts=%+v", manager.starts)
	}
}

func TestReadyModelContinuesIntoDiscoveryAndFocusesRoleSelection(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
	var generation uint64
	discover := func(_ context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		generation = gen
		return func() tea.Msg {
			return DiscoveryMsg{Generation: gen, Results: []setup.EndpointResult{
				{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: []discovery.Model{{ID: "gemma"}}},
				{Endpoint: topology.Endpoint{ID: "lm-studio-local", Name: "LM Studio"}, Models: []discovery.Model{{ID: "alpha"}, {ID: "beta"}}},
			}}
		}
	}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager, Discover: discover})
	m, inspect := update(m, key("ctrl+r"))
	m, _ = update(m, inspect())
	m, _ = update(m, key("right"))
	m, command := update(m, key("enter"))
	if m.localModels.open || !m.setup || m.screen != setup.StateWelcome || command == nil || generation == 0 {
		t.Fatalf("did not enter setup discovery: setup=%v screen=%v", m.setup, m.screen)
	}
	m, _ = update(m, command())
	if m.screen != setup.StateRoles || m.modelIndex != 1 {
		t.Fatalf("ready model did not go directly to focused role assignment: screen=%v index=%d", m.screen, m.modelIndex)
	}
}

func TestLocalModelBrowserRejectsUnavailableStatesAndSurfacesStartErrors(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures(), err: errors.New("model failed to load")}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager})
	m.running = true
	m, _ = update(m, key("ctrl+r"))
	if m.localModels.open {
		t.Fatal("local models opened during orchestration")
	}
	m = NewWithServices(config.Default(), Services{LocalModels: manager})
	m.screen = setup.StateRoles
	m.workflow.State = setup.StateRoles
	m, _ = update(m, key("ctrl+r"))
	if m.localModels.open {
		t.Fatal("local models opened during role assignment")
	}

	m = NewWithServices(completeConfig(), Services{LocalModels: manager})
	m, inspect := update(m, key("ctrl+r"))
	m, _ = update(m, inspect())
	m, _ = update(m, key("s"))
	m, start := update(m, key("y"))
	m, refresh := update(m, start())
	if refresh != nil || !strings.Contains(m.View().Content, "model failed to load") {
		t.Fatalf("start error missing: %s", m.View().Content)
	}
}

func TestLocalModelBrowserIgnoresStaleInspection(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: runtimeFixtures()}
	m := NewWithServices(completeConfig(), Services{LocalModels: manager})
	m, staleCommand := update(m, key("ctrl+r"))
	m, _ = update(m, key("esc"))
	m, currentCommand := update(m, key("ctrl+r"))
	m, _ = update(m, staleCommand())
	if len(m.localModels.runtimes) != 0 || !m.localModels.loading {
		t.Fatalf("stale inspection applied: %+v", m.localModels)
	}
	m, _ = update(m, currentCommand())
	if len(m.localModels.runtimes) != 3 || m.localModels.loading {
		t.Fatalf("current inspection missing: %+v", m.localModels)
	}
}
