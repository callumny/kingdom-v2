package setup

import (
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestCatalogKeepsSameModelIDFromDifferentProvidersDistinct(t *testing.T) {
	draft := NewDraft(config.Default(), nil)
	draft.ApplyResults([]EndpointResult{
		{
			Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
			Models:   []discovery.Model{{ID: "shared", SizeBytes: 2_000_000_000, ParameterSize: "3B"}},
		},
		{
			Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"},
			Models:   []discovery.Model{{ID: "shared", SizeBytes: 5_000_000_000, ParameterSize: "7B"}},
		},
	})

	catalog := draft.Catalog()
	if len(catalog) != 2 {
		t.Fatalf("catalog length=%d, want 2: %+v", len(catalog), catalog)
	}
	if catalog[0].Ref != (ModelRef{EndpointID: "ollama-local", ModelID: "shared"}) {
		t.Fatalf("first ref=%+v", catalog[0].Ref)
	}
	if catalog[1].Ref != (ModelRef{EndpointID: "mlx-local", ModelID: "shared"}) {
		t.Fatalf("second ref=%+v", catalog[1].Ref)
	}
	if catalog[0].Endpoint.Name != "Ollama" || catalog[1].Endpoint.Name != "MLX" {
		t.Fatalf("provider metadata lost: %+v", catalog)
	}
	if catalog[0].SizeBytes != 2_000_000_000 || catalog[1].ParameterSize != "7B" {
		t.Fatalf("model metadata lost: %+v", catalog)
	}
}

func TestCatalogIsStableAndDropsInvalidOrDuplicateEntries(t *testing.T) {
	draft := NewDraft(config.Default(), nil)
	draft.ApplyResults([]EndpointResult{{
		Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
		Models: []discovery.Model{
			{ID: "first"},
			{ID: ""},
			{ID: "first"},
			{ID: "second"},
		},
	}})

	catalog := draft.Catalog()
	if len(catalog) != 2 || catalog[0].Ref.ModelID != "first" || catalog[1].Ref.ModelID != "second" {
		t.Fatalf("catalog=%+v, want stable first/second", catalog)
	}
}
