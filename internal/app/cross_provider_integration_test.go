package app

import (
	"path/filepath"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestSetupSavesRolesAcrossOllamaAndMLX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	configuration := config.Default()
	results := []setup.EndpointResult{
		{
			Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://localhost:11434"},
			Models:   []discovery.Model{{ID: "small", ParameterSize: "3B", SizeBytes: 2_000_000_000}},
		},
		{
			Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://localhost:8080/v1"},
			Models:   []discovery.Model{{ID: "large", ParameterSize: "14B", SizeBytes: 9_000_000_000}},
		},
	}
	defaults := []topology.Endpoint{results[0].Endpoint, results[1].Endpoint}
	m := NewWithDepsAndSave(configuration, defaults, nil, func(c config.Config) error { return config.Save(path, c) })
	m.workflow.Draft.ApplyResults(results)
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.Config.Providers.MLX.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m.workflow.Draft.SetProviderReady(setup.MLXEndpointID, true)

	m, _ = update(m, key("enter"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	roles := m.workflow.Draft.Config.Topology.Roles
	if roles.King.EndpointID != "mlx-local" || roles.Worker.EndpointID != "ollama-local" {
		t.Fatalf("cross-provider suggestions=%+v", roles)
	}

	m, _ = update(m, key("n"))
	m, _ = update(m, key("enter"))
	m, save := update(m, key("enter"))
	if save == nil {
		t.Fatal("save command is nil")
	}
	m, _ = update(m, save())
	if m.screen != setup.StateReady {
		t.Fatalf("screen=%v, want ready", m.screen)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Topology.Roles.King.EndpointID != "mlx-local" || loaded.Topology.Roles.Worker.EndpointID != "ollama-local" {
		t.Fatalf("saved roles=%+v", loaded.Topology.Roles)
	}
	seen := map[string]bool{}
	for _, endpoint := range loaded.Topology.Endpoints {
		seen[endpoint.ID] = true
	}
	if !seen["ollama-local"] || !seen["mlx-local"] {
		t.Fatalf("saved endpoints=%+v", loaded.Topology.Endpoints)
	}
}
