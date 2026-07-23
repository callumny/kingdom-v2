package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
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

func TestModelsViewKeepsDownloadProgressInContext(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	draft.ApplyResults([]setup.EndpointResult{{
		Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"},
		Models:   []discovery.Model{{ID: "large", ParameterSize: "14B"}},
	}})
	if err := draft.ToggleModel(setup.ModelRef{EndpointID: "mlx-local", ModelID: "large"}); err != nil {
		t.Fatal(err)
	}
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 38, true, w, Presentation{
		ModelDownloadActive:   true,
		ModelDownloadPosition: 1,
		ModelDownloadCount:    3,
		ModelDownloadProgress: localmodels.DownloadProgress{
			Provider:        localmodels.KindMLX,
			Model:           "large",
			Status:          "Downloading MLX model",
			Percent:         38,
			DownloadedBytes: 1_900_000_000,
			TotalBytes:      5_000_000_000,
			BytesPerSecond:  25_000_000,
			ETA:             2*time.Minute + 8*time.Second,
		},
	}).Content
	for _, want := range []string{
		"Preparing your models",
		"Model 1 of 3",
		"MLX · large",
		"Downloading MLX model · 38%",
		"1.9 GB / 5.0 GB",
		"Estimated 25.0 MB/s",
		"2m 8s remaining",
		"opens the Wizard automatically",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("model download view missing %q: %s", want, view)
		}
	}
}

func TestModelsViewShowsDownloadFailureAndRetryGuidance(t *testing.T) {
	draft := setup.NewDraft(config.Default(), nil)
	option := setup.ModelOption{
		Ref:      setup.ModelRef{EndpointID: "ollama-local", ModelID: "qwen3:8b"},
		Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
	}
	draft.ReplaceCatalog([]setup.ModelOption{option})
	if err := draft.ToggleModel(option.Ref); err != nil {
		t.Fatal(err)
	}
	w := &setup.Workflow{State: setup.StateModels, Draft: draft}

	view := ViewWithPresentation(100, 38, true, w, Presentation{ModelDownloadError: "network unavailable"}).Content
	for _, want := range []string{"Download failed", "network unavailable", "retry"} {
		if !strings.Contains(view, want) {
			t.Fatalf("download failure view missing %q: %s", want, view)
		}
	}
}
