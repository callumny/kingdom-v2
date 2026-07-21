package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestModelsViewCombinesProvidersAndShowsSelectionLimit(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: []discovery.Model{{ID: "small", SizeBytes: 2_000_000_000, ParameterSize: "3B"}}},
		{Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}, Models: []discovery.Model{{ID: "large", SizeBytes: 8_000_000_000, ParameterSize: "7B"}}},
	})
	_ = draft.ToggleModel(setup.ModelRef{EndpointID: "mlx-local", ModelID: "large"})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 32, true, w, Presentation{ModelCursor: 1}).Content
	for _, want := range []string{"Choose your models", "1 of 3 selected", "Ollama", "small", "MLX", "large", "3B", "7B", "Space Toggle"} {
		if !strings.Contains(view, want) {
			t.Fatalf("models view missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "m Manage providers") {
		t.Fatalf("legacy model-management shortcut is still advertised: %s", view)
	}
	assertViewFits(t, view, 100, 32)
}

func TestModelsViewScrollsAWindowAroundTheCursor(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	models := make([]discovery.Model, 12)
	for index := range models {
		models[index].ID = fmt.Sprintf("model-%02d", index)
	}
	draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: models}})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 40, true, w, Presentation{ModelCursor: 10}).Content
	if !strings.Contains(view, "model-10") || !strings.Contains(view, "Showing 5–12 of 12") || strings.Contains(view, "model-00") {
		t.Fatalf("model window did not follow cursor: %s", view)
	}
}
