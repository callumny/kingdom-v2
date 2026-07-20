package ui

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestRolesViewExplainsJobsAndSizeGuidance(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: []discovery.Model{{ID: "small", ParameterSize: "3B"}}},
		{Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}, Models: []discovery.Model{{ID: "large", ParameterSize: "14B"}}},
	})
	_ = draft.ToggleModel(setup.ModelRef{EndpointID: "ollama-local", ModelID: "small"})
	_ = draft.ToggleModel(setup.ModelRef{EndpointID: "mlx-local", ModelID: "large"})
	_ = draft.ApplyRoleSuggestions()
	w := &setup.Workflow{State: setup.StateRoles, Draft: draft}

	view := ViewWithPresentation(100, 34, true, w, Presentation{}).Content
	for _, want := range []string{
		"Assign models to roles",
		"King",
		"plans and coordinates",
		"larger model",
		"Worker",
		"focused tasks",
		"smaller model",
		"Council",
		"independent review",
		"MLX / large",
		"Ollama / small",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("role guidance missing %q: %s", want, view)
		}
	}
}
