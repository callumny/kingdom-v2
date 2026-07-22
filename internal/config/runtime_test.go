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

func TestRuntimePlanAssignsOnePortPerUniqueMLXModel(t *testing.T) {
	cfg := managedMLXConfig()
	cfg.Providers.MLX.Port = 13000

	plan, err := BuildRuntimePlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.MLXRoutes) != 2 {
		t.Fatalf("routes=%+v", plan.MLXRoutes)
	}
	if plan.MLXRoutes[0].Model != "large" || plan.MLXRoutes[0].Endpoint.BaseURL != "http://127.0.0.1:13000/v1" {
		t.Fatalf("first route=%+v", plan.MLXRoutes[0])
	}
	if plan.MLXRoutes[1].Model != "small" || plan.MLXRoutes[1].Endpoint.BaseURL != "http://127.0.0.1:13001/v1" {
		t.Fatalf("second route=%+v", plan.MLXRoutes[1])
	}
	roles := plan.Config.Topology.Roles
	if roles.King.EndpointID == "mlx-local" || roles.Worker.EndpointID == "mlx-local" || roles.King.EndpointID == roles.Worker.EndpointID {
		t.Fatalf("MLX assignments=%+v", roles)
	}
	if roles.Council.EndpointID != roles.King.EndpointID {
		t.Fatalf("same MLX model did not reuse server: %+v", roles)
	}
	if cfg.Topology.Roles.King.EndpointID != "mlx-local" {
		t.Fatal("runtime planning mutated persisted configuration")
	}
}

func TestRuntimePlanRejectsMLXPortOverflow(t *testing.T) {
	cfg := managedMLXConfig()
	cfg.Providers.MLX.Port = 65535

	if _, err := BuildRuntimePlan(cfg); err == nil || !strings.Contains(err.Error(), "MLX does not have enough consecutive ports") {
		t.Fatalf("overflow error=%v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "MLX does not have enough consecutive ports") {
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

func managedMLXConfig() Config {
	cfg := Default()
	cfg.Providers.MLX.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: "mlx-local", Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: "mlx-local", Model: "large"}
	cfg.Topology.Roles.Worker = topology.Assignment{EndpointID: "mlx-local", Model: "small"}
	cfg.Topology.Roles.Council = topology.Assignment{EndpointID: "mlx-local", Model: "large"}
	return cfg
}
