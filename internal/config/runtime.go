package config

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

const managedOllamaEndpointID = "ollama-local"

// OllamaRoute maps one selected model to the endpoint used for this process.
// Routes are derived at runtime and are never persisted.
type OllamaRoute struct {
	Model    string
	Endpoint topology.Endpoint
}

// RuntimePlan contains an ephemeral configuration and the Ollama servers it
// requires. The persisted configuration remains provider- and model-oriented.
type RuntimePlan struct {
	Config       Config
	OllamaRoutes []OllamaRoute
}

// BuildRuntimePlan resolves managed Ollama assignments to deterministic local
// endpoints. Dedicated mode uses one endpoint per unique active model; shared
// mode routes every model through the configured base port.
func BuildRuntimePlan(persisted Config) (RuntimePlan, error) {
	if err := persisted.Validate(); err != nil {
		return RuntimePlan{}, err
	}
	runtimeConfig := persisted
	runtimeConfig.Topology.Endpoints = append([]topology.Endpoint(nil), persisted.Topology.Endpoints...)
	base, baseIndex, found := managedOllamaEndpoint(runtimeConfig.Topology.Endpoints)
	models := managedOllamaModels(runtimeConfig)
	if len(models) == 0 {
		return RuntimePlan{Config: runtimeConfig}, nil
	}
	if !found || base.Kind != topology.KindOllama {
		return RuntimePlan{}, fmt.Errorf("managed Ollama endpoint is unavailable")
	}
	base.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", persisted.Providers.Ollama.Port)
	runtimeConfig.Topology.Endpoints[baseIndex] = base

	routes := make([]OllamaRoute, 0, len(models))
	if persisted.Providers.Ollama.PortMode == OllamaSharedPort {
		for _, model := range models {
			routes = append(routes, OllamaRoute{Model: model, Endpoint: base})
		}
		return RuntimePlan{Config: runtimeConfig, OllamaRoutes: routes}, nil
	}

	byModel := make(map[string]topology.Endpoint, len(models))
	for index, model := range models {
		endpoint := topology.Endpoint{
			ID:      runtimeOllamaEndpointID(model),
			Name:    "Ollama · " + model,
			Kind:    topology.KindOllama,
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d", persisted.Providers.Ollama.Port+index),
		}
		byModel[model] = endpoint
		routes = append(routes, OllamaRoute{Model: model, Endpoint: endpoint})
		runtimeConfig.Topology.Endpoints = append(runtimeConfig.Topology.Endpoints, endpoint)
	}
	rewriteManagedOllamaAssignments(&runtimeConfig, byModel)
	if err := runtimeConfig.Validate(); err != nil {
		return RuntimePlan{}, fmt.Errorf("validate runtime topology: %w", err)
	}
	return RuntimePlan{Config: runtimeConfig, OllamaRoutes: routes}, nil
}

func validateOllamaPortCapacity(c Config) error {
	if c.Providers.Ollama.PortMode != OllamaDedicatedPorts {
		return nil
	}
	count := len(managedOllamaModels(c))
	if count > 0 && c.Providers.Ollama.Port+count-1 > 65535 {
		return fmt.Errorf("Ollama does not have enough consecutive ports: needs %d starting at %d; choose a lower base port", count, c.Providers.Ollama.Port)
	}
	return nil
}

func managedOllamaModels(c Config) []string {
	assignments := []topology.Assignment{c.Topology.Roles.King, c.Topology.Roles.Worker}
	if c.CouncilEnabled {
		assignments = append(assignments, c.Topology.Roles.Council)
	}
	seen := make(map[string]bool, len(assignments))
	models := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		model := strings.TrimSpace(assignment.Model)
		if assignment.EndpointID != managedOllamaEndpointID || model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

func rewriteManagedOllamaAssignments(c *Config, endpoints map[string]topology.Endpoint) {
	assignments := []*topology.Assignment{&c.Topology.Roles.King, &c.Topology.Roles.Worker}
	if c.CouncilEnabled {
		assignments = append(assignments, &c.Topology.Roles.Council)
	}
	for _, assignment := range assignments {
		if assignment.EndpointID == managedOllamaEndpointID {
			assignment.EndpointID = endpoints[strings.TrimSpace(assignment.Model)].ID
		}
	}
}

func managedOllamaEndpoint(endpoints []topology.Endpoint) (topology.Endpoint, int, bool) {
	for index, endpoint := range endpoints {
		if endpoint.ID == managedOllamaEndpointID {
			return endpoint, index, true
		}
	}
	return topology.Endpoint{}, 0, false
}

func runtimeOllamaEndpointID(model string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(model)))
	return fmt.Sprintf("ollama-runtime-%x", digest[:8])
}
