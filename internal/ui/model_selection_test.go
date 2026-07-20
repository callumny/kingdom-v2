package ui

import (
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
	assertViewFits(t, view, 100, 32)
}
