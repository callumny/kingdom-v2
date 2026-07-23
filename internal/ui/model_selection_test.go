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

func TestModelsViewRecommendsMLXPerformanceOnlyWhenEnabled(t *testing.T) {
	withMLX := config.Default()
	withMLX.Providers.MLX.Enabled = true
	draft := setup.NewDraft(withMLX, nil)
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}
	view := ViewWithPresentation(100, 32, true, w, Presentation{}).Content
	if !strings.Contains(view, "MLX models generally run faster on Apple silicon") {
		t.Fatalf("MLX Apple silicon guidance missing: %s", view)
	}

	ollamaOnly := config.Default()
	ollamaOnly.Providers.Ollama.Enabled = true
	draft = setup.NewDraft(ollamaOnly, nil)
	w = &setup.Workflow{State: setup.StateModels, Draft: draft}
	view = ViewWithPresentation(100, 32, true, w, Presentation{}).Content
	if strings.Contains(view, "MLX models generally run faster on Apple silicon") {
		t.Fatalf("MLX guidance shown when MLX is disabled: %s", view)
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

func TestModelsViewGroupsPopularDownloadsByProvider(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ReplaceCatalog([]setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "llama3.1:8b"}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"}, Installed: true, ParameterSize: "8B"},
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "deepseek-r1:8b"}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"}, ParameterSize: "8B", PopularityRank: 1, PopularityDownloads: 90_300_000},
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "llama3.2"}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"}, ParameterSize: "3B", PopularityRank: 2, PopularityDownloads: 77_500_000},
		{Ref: setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: "mlx-community/Qwen3-8B-4bit"}, Endpoint: topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX"}, ParameterSize: "8B", PopularityRank: 1, PopularityDownloads: 900_000},
		{Ref: setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: "mlx-community/Qwen3-4B-4bit"}, Endpoint: topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX"}, ParameterSize: "4B", PopularityRank: 2, PopularityDownloads: 600_000},
	})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(120, 42, true, w, Presentation{}).Content
	for _, want := range []string{
		"Installed models",
		"Popular downloads",
		"Popular on Ollama",
		"Popular on MLX",
		"deepseek-r1:8b",
		"mlx-community/Qwen3-8B-4bit",
		"#1 popular available",
		"90.3M pulls",
		"900K downloads",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("popular models view missing %q: %s", want, view)
		}
	}
	installed := strings.Index(view, "llama3.1:8b")
	ollamaHeading := strings.Index(view, "Popular on Ollama")
	ollamaLast := strings.Index(view, "llama3.2")
	mlxHeading := strings.Index(view, "Popular on MLX")
	mlxFirst := strings.Index(view, "mlx-community/Qwen3-8B-4bit")
	if !(installed < ollamaHeading && ollamaHeading < ollamaLast && ollamaLast < mlxHeading && mlxHeading < mlxFirst) {
		t.Fatalf("popular provider groups are interwoven: %s", view)
	}
}

func TestModelsViewShowsNonBlockingPopularLoadingAndWarnings(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ReplaceCatalog([]setup.ModelOption{{
		Ref:       setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "llama3.1:8b"},
		Endpoint:  topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
		Installed: true,
	}})
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	loading := ViewWithPresentation(100, 32, true, w, Presentation{ModelPopularLoading: true}).Content
	if !strings.Contains(loading, "Finding popular Ollama and MLX models") || !strings.Contains(loading, "llama3.1:8b") {
		t.Fatalf("popular loading blocked installed models: %s", loading)
	}
	warning := ViewWithPresentation(100, 32, true, w, Presentation{ModelPopularWarning: "MLX popularity temporarily unavailable"}).Content
	if !strings.Contains(warning, "MLX popularity temporarily unavailable") || !strings.Contains(warning, "Press / to search") {
		t.Fatalf("popular warning missing fallback: %s", warning)
	}
}

func TestModelsViewShowsTheCompletePopularCatalogueAtPresentationHeight(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	options := make([]setup.ModelOption, 0, 12)
	for index := 1; index <= 6; index++ {
		options = append(options, setup.ModelOption{
			Ref:       setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: fmt.Sprintf("installed-%d", index)},
			Endpoint:  topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
			Installed: true,
		})
	}
	for index := 1; index <= 3; index++ {
		options = append(options, setup.ModelOption{
			Ref:            setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: fmt.Sprintf("popular-ollama-%d", index)},
			Endpoint:       topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
			PopularityRank: index,
		})
	}
	for index := 1; index <= 3; index++ {
		options = append(options, setup.ModelOption{
			Ref:            setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: fmt.Sprintf("mlx-community/popular-model-%d-4bit", index)},
			Endpoint:       topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX"},
			PopularityRank: index,
		})
	}
	draft.ReplaceCatalog(options)
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(187, 49, true, w, Presentation{}).Content
	for _, want := range []string{"installed-6", "Popular on Ollama", "popular-ollama-3", "Popular on MLX", "mlx-community/popular-model-3-4bit", "Enter Continue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("full models screen missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "Showing 1–") {
		t.Fatalf("presentation-height screen unnecessarily windowed the catalogue: %s", view)
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
	for _, want := range []string{"Download selected model?", "MLX", "What happens next", "opens the Wizard to complete setup"} {
		if !strings.Contains(view, want) {
			t.Fatalf("download confirmation missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "tests the selected models") {
		t.Fatalf("download confirmation still promises the removed benchmark step: %s", view)
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
