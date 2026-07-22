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
	for _, want := range []string{
		"Choose your models",
		"Installed models",
		"2 found across 2 providers",
		"1 of 3 selected",
		"Ollama",
		"small",
		"MLX",
		"large",
		"3B",
		"7B",
		"Installed models are always shown first",
		"Space Select",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("models view missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "m Manage providers") {
		t.Fatalf("legacy model-management shortcut is still advertised: %s", view)
	}
	assertViewFits(t, view, 100, 32)
}

func TestModelsViewAdvertisesUninstallWithoutClippingNavigation(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ReplaceCatalog([]setup.ModelOption{{
		Ref:       setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "qwen3:8b"},
		Endpoint:  topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
		Installed: true,
	}})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}
	view := ViewWithPresentation(80, 30, true, w, Presentation{}).Content
	for _, want := range []string{"d Uninstall", "Enter Continue", "Esc Back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("models footer missing %q: %s", want, view)
		}
	}
}

func TestModelsViewGivesSearchResultsTheirOwnHierarchy(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ReplaceCatalog([]setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: "ollama-local", ModelID: "qwen3:8b"}, Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Installed: true, ParameterSize: "8B"},
		{Ref: setup.ModelRef{EndpointID: "ollama-local", ModelID: "qwen3:14b"}, Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}},
		{Ref: setup.ModelRef{EndpointID: "mlx-local", ModelID: "mlx-community/Qwen3-4B-4bit"}, Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}},
	})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 36, true, w, Presentation{ModelQuery: "qwen", ModelSearchActive: true}).Content
	for _, want := range []string{"Search models", "Results for “qwen”", "Ollama ✓", "MLX ✓", "3 matches", "online matches"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search view missing %q: %s", want, view)
		}
	}
}

func TestModelsViewSummarisesACompleteMixedSelection(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	options := []setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: "ollama-local", ModelID: "small"}, Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Installed: true, ParameterSize: "3B"},
		{Ref: setup.ModelRef{EndpointID: "ollama-local", ModelID: "medium"}, Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Installed: true, ParameterSize: "8B"},
		{Ref: setup.ModelRef{EndpointID: "mlx-local", ModelID: "large"}, Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}, ParameterSize: "14B"},
	}
	draft.ReplaceCatalog(options)
	for _, option := range options {
		if err := draft.ToggleModel(option.Ref); err != nil {
			t.Fatal(err)
		}
	}
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 38, true, w, Presentation{}).Content
	for _, want := range []string{"3 models selected", "3B · 8B · 14B", "slower and use", "more RAM", "One download required"} {
		if !strings.Contains(view, want) {
			t.Fatalf("selection summary missing %q: %s", want, view)
		}
	}
}

func TestModelDownloadConfirmationExplainsTheNextStep(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	option := setup.ModelOption{Ref: setup.ModelRef{EndpointID: "mlx-local", ModelID: "mlx-community/Qwen3-14B-4bit"}, Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}}
	draft.ReplaceCatalog([]setup.ModelOption{option})
	if err := draft.ToggleModel(option.Ref); err != nil {
		t.Fatal(err)
	}
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 32, true, w, Presentation{ModelDownloadConfirming: true}).Content
	for _, want := range []string{"Download selected model?", "MLX", "What happens next", "tests the selected models", "opens the Wizard"} {
		if !strings.Contains(view, want) {
			t.Fatalf("download confirmation missing %q: %s", want, view)
		}
	}
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
