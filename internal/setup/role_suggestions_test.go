package setup

import (
	"testing"

	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestRoleSuggestionsUseLargestForKingAndSmallestForWorker(t *testing.T) {
	draft := selectionDraft()
	for _, ref := range []ModelRef{
		{EndpointID: "ollama-local", ModelID: "small"},
		{EndpointID: "ollama-local", ModelID: "medium"},
		{EndpointID: "mlx-local", ModelID: "large"},
	} {
		if err := draft.ToggleModel(ref); err != nil {
			t.Fatal(err)
		}
	}

	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	roles := draft.Config.Topology.Roles
	if roles.King != (topology.Assignment{EndpointID: "mlx-local", Model: "large"}) {
		t.Fatalf("king=%+v", roles.King)
	}
	if roles.Worker != (topology.Assignment{EndpointID: "ollama-local", Model: "small"}) {
		t.Fatalf("worker=%+v", roles.Worker)
	}
	if roles.Council != (topology.Assignment{EndpointID: "ollama-local", Model: "medium"}) || !draft.Config.CouncilEnabled {
		t.Fatalf("council=%+v enabled=%v", roles.Council, draft.Config.CouncilEnabled)
	}
}

func TestRoleSuggestionsHandleOneAndTwoModels(t *testing.T) {
	tests := []struct {
		name           string
		refs           []ModelRef
		king           string
		worker         string
		councilEnabled bool
	}{
		{name: "one", refs: []ModelRef{{EndpointID: "ollama-local", ModelID: "small"}}, king: "small", worker: "small"},
		{name: "two", refs: []ModelRef{{EndpointID: "ollama-local", ModelID: "small"}, {EndpointID: "mlx-local", ModelID: "large"}}, king: "large", worker: "small"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := selectionDraft()
			for _, ref := range tt.refs {
				_ = draft.ToggleModel(ref)
			}
			if err := draft.ApplyRoleSuggestions(); err != nil {
				t.Fatal(err)
			}
			if draft.Config.Topology.Roles.King.Model != tt.king || draft.Config.Topology.Roles.Worker.Model != tt.worker || draft.Config.CouncilEnabled != tt.councilEnabled {
				t.Fatalf("roles=%+v council enabled=%v", draft.Config.Topology.Roles, draft.Config.CouncilEnabled)
			}
		})
	}
}

func TestRoleSuggestionsPreserveValidExistingAssignments(t *testing.T) {
	draft := selectionDraft()
	refs := []ModelRef{{EndpointID: "ollama-local", ModelID: "small"}, {EndpointID: "mlx-local", ModelID: "large"}}
	for _, ref := range refs {
		_ = draft.ToggleModel(ref)
	}
	draft.AssignKing(refs[0].Assignment())
	draft.AssignWorker(refs[1].Assignment())
	draft.SetCouncilEnabled(false)

	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	if draft.Config.Topology.Roles.King != refs[0].Assignment() || draft.Config.Topology.Roles.Worker != refs[1].Assignment() {
		t.Fatalf("valid custom assignments were overwritten: %+v", draft.Config.Topology.Roles)
	}
}

func TestRoleSuggestionsInferParameterScaleFromModelID(t *testing.T) {
	draft := selectionDraft()
	draft.Results[0].Models = []discovery.Model{{ID: "qwen-3b"}}
	draft.Results[1].Models = []discovery.Model{{ID: "mlx-community/qwen-14B-instruct"}}
	large := ModelRef{EndpointID: "mlx-local", ModelID: "mlx-community/qwen-14B-instruct"}
	small := ModelRef{EndpointID: "ollama-local", ModelID: "qwen-3b"}
	_ = draft.ToggleModel(large)
	_ = draft.ToggleModel(small)

	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	if draft.Config.Topology.Roles.King != large.Assignment() || draft.Config.Topology.Roles.Worker != small.Assignment() {
		t.Fatalf("roles=%+v", draft.Config.Topology.Roles)
	}
}
