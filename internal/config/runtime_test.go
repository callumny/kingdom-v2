package config

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/topology"
)

func TestRuntimePlanAssignsOnePortPerUniqueOllamaModel(t *testing.T) {
	cfg := managedOllamaConfig()
	cfg.Providers.Ollama.Port = 12000
	cfg.Providers.Ollama.PortMode = OllamaDedicatedPorts

	plan, err := BuildRuntimePlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.OllamaRoutes) != 2 {
		t.Fatalf("routes=%+v", plan.OllamaRoutes)
	}
	if plan.OllamaRoutes[0].Model != "large" || plan.OllamaRoutes[0].Endpoint.BaseURL != "http://127.0.0.1:12000" {
		t.Fatalf("first route=%+v", plan.OllamaRoutes[0])
	}
	if plan.OllamaRoutes[1].Model != "small" || plan.OllamaRoutes[1].Endpoint.BaseURL != "http://127.0.0.1:12001" {
		t.Fatalf("second route=%+v", plan.OllamaRoutes[1])
	}
	roles := plan.Config.Topology.Roles
	if roles.King.EndpointID == "ollama-local" || roles.Worker.EndpointID == "ollama-local" || roles.King.EndpointID == roles.Worker.EndpointID {
		t.Fatalf("dedicated assignments=%+v", roles)
	}
	if roles.Council.EndpointID != roles.King.EndpointID {
		t.Fatalf("same model did not reuse server: %+v", roles)
	}
	if cfg.Topology.Roles.King.EndpointID != "ollama-local" {
		t.Fatal("runtime planning mutated persisted configuration")
	}
	if err := plan.Config.Validate(); err != nil {
		t.Fatalf("runtime config invalid: %v", err)
	}
}

func TestRuntimePlanCanShareTheConfiguredOllamaPort(t *testing.T) {
	cfg := managedOllamaConfig()
	cfg.Providers.Ollama.Port = 12000
	cfg.Providers.Ollama.PortMode = OllamaSharedPort

	plan, err := BuildRuntimePlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.OllamaRoutes) != 2 {
		t.Fatalf("routes=%+v", plan.OllamaRoutes)
	}
	for _, route := range plan.OllamaRoutes {
		if route.Endpoint.ID != "ollama-local" || route.Endpoint.BaseURL != "http://127.0.0.1:12000" {
			t.Fatalf("shared route=%+v", route)
		}
	}
	if plan.Config.Topology.Roles != cfg.Topology.Roles {
		t.Fatalf("shared mode rewrote assignments: %+v", plan.Config.Topology.Roles)
	}
}

func TestRuntimePlanRejectsDedicatedPortOverflow(t *testing.T) {
	cfg := managedOllamaConfig()
	cfg.Providers.Ollama.Port = 65535
	cfg.Providers.Ollama.PortMode = OllamaDedicatedPorts

	if _, err := BuildRuntimePlan(cfg); err == nil || !strings.Contains(err.Error(), "enough consecutive ports") {
		t.Fatalf("overflow error=%v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "enough consecutive ports") {
		t.Fatalf("validation error=%v", err)
	}
}

func managedOllamaConfig() Config {
	cfg := Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: "ollama-local", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: "ollama-local", Model: "large"}
	cfg.Topology.Roles.Worker = topology.Assignment{EndpointID: "ollama-local", Model: "small"}
	cfg.Topology.Roles.Council = topology.Assignment{EndpointID: "ollama-local", Model: "large"}
	return cfg
}
