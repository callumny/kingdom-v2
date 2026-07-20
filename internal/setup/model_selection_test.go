package setup

import (
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestModelSelectionSpansProvidersAndStopsAtThree(t *testing.T) {
	draft := selectionDraft()
	refs := []ModelRef{
		{EndpointID: "ollama-local", ModelID: "small"},
		{EndpointID: "mlx-local", ModelID: "large"},
		{EndpointID: "ollama-local", ModelID: "medium"},
		{EndpointID: "mlx-local", ModelID: "other"},
	}
	for _, ref := range refs[:3] {
		if err := draft.ToggleModel(ref); err != nil {
			t.Fatalf("select %+v: %v", ref, err)
		}
	}
	if err := draft.ToggleModel(refs[3]); err == nil {
		t.Fatal("selected a fourth model")
	}
	selected := draft.SelectedModels()
	if len(selected) != 3 || selected[0].Ref != refs[0] || selected[2].Ref != refs[2] {
		t.Fatalf("selected=%+v", selected)
	}
	if err := draft.ToggleModel(refs[0]); err != nil || draft.IsModelSelected(refs[0]) {
		t.Fatalf("deselect failed: %v", err)
	}
}

func TestModelSelectionRejectsUnknownModelAndReconcilesRescan(t *testing.T) {
	draft := selectionDraft()
	missing := ModelRef{EndpointID: "missing", ModelID: "model"}
	if err := draft.ToggleModel(missing); err == nil {
		t.Fatal("selected an unknown model")
	}
	keep := ModelRef{EndpointID: "ollama-local", ModelID: "small"}
	remove := ModelRef{EndpointID: "mlx-local", ModelID: "large"}
	_ = draft.ToggleModel(keep)
	_ = draft.ToggleModel(remove)
	draft.ApplyResults(draft.Results[:1])
	removed := draft.ReconcileModelSelection()
	if len(removed) != 1 || removed[0] != remove || !draft.IsModelSelected(keep) {
		t.Fatalf("removed=%+v selected=%+v", removed, draft.SelectedModels())
	}
}

func TestWorkflowIncludesModelsBetweenProvidersAndRoles(t *testing.T) {
	draft := selectionDraft()
	w := &Workflow{State: StateProviders, Draft: draft}
	if err := w.Continue(); err != nil || w.State != StateModels {
		t.Fatalf("providers continue state=%v err=%v", w.State, err)
	}
	if err := w.Continue(); err == nil || w.State != StateModels {
		t.Fatalf("models advanced without a selection: state=%v err=%v", w.State, err)
	}
	if err := w.Draft.ToggleModel(ModelRef{EndpointID: "mlx-local", ModelID: "large"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Continue(); err != nil || w.State != StateRoles {
		t.Fatalf("models continue state=%v err=%v", w.State, err)
	}
	w.Back()
	if w.State != StateModels {
		t.Fatalf("roles back=%v, want models", w.State)
	}
}

func selectionDraft() Draft {
	draft := NewDraft(config.Default(), nil)
	draft.Config.Providers.Ollama.Enabled = true
	draft.ApplyResults([]EndpointResult{
		{
			Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
			Models: []discovery.Model{
				{ID: "small", SizeBytes: 2_000_000_000, ParameterSize: "3B"},
				{ID: "medium", SizeBytes: 5_000_000_000, ParameterSize: "7B"},
			},
		},
		{
			Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"},
			Models: []discovery.Model{
				{ID: "large", SizeBytes: 10_000_000_000, ParameterSize: "14B"},
				{ID: "other", SizeBytes: 12_000_000_000, ParameterSize: "20B"},
			},
		},
	})
	return draft
}
