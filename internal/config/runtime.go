package config

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

const managedOllamaEndpointID = "ollama-local"
const managedMLXEndpointID = "mlx-local"

// OllamaRoute maps one selected model to the endpoint used for this process.
// Routes are derived at runtime and are never persisted.
type OllamaRoute struct {
	Model    string
	Endpoint topology.Endpoint
}

// MLXRoute maps one selected model to its dedicated local server.
type MLXRoute struct {
	Model    string
	Endpoint topology.Endpoint
}

// RuntimePlan contains an ephemeral configuration and the Ollama servers it
// requires. The persisted configuration remains provider- and model-oriented.
type RuntimePlan struct {
	Config       Config
	OllamaRoutes []OllamaRoute
	MLXRoutes    []MLXRoute
}

// UsesManagedOllama reports whether an active role uses Kingdom's managed
// Ollama provider. Custom Ollama-compatible endpoints are intentionally not
// included because Kingdom does not own their processes or ports.
func UsesManagedOllama(c Config) bool {
	return len(managedOllamaModels(c)) > 0
}

// ValidateOllamaPortPlan checks that dedicated mode can allocate one
// consecutive port per unique managed model.
func ValidateOllamaPortPlan(c Config) error {
	return validateOllamaPortCapacity(c)
}

// ValidateRuntimePortPlan checks that every managed provider has enough
// loopback ports for the active model assignments.
func ValidateRuntimePortPlan(c Config) error {
	return validateOllamaPortCapacity(c)
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
	plan := RuntimePlan{}
	ollamaModels := managedOllamaModels(runtimeConfig)
	if len(ollamaModels) > 0 {
		base, baseIndex, found := managedEndpoint(runtimeConfig.Topology.Endpoints, managedOllamaEndpointID)
		if !found || base.Kind != topology.KindOllama {
			return RuntimePlan{}, fmt.Errorf("managed Ollama endpoint is unavailable")
		}
		base.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", persisted.Providers.Ollama.Port)
		runtimeConfig.Topology.Endpoints[baseIndex] = base
		if persisted.Providers.Ollama.PortMode == OllamaSharedPort {
			for _, model := range ollamaModels {
				plan.OllamaRoutes = append(plan.OllamaRoutes, OllamaRoute{Model: model, Endpoint: base})
			}
		} else {
			byModel := make(map[string]topology.Endpoint, len(ollamaModels))
			for index, model := range ollamaModels {
				endpoint := topology.Endpoint{ID: runtimeEndpointID("ollama", model), Name: "Ollama · " + model, Kind: topology.KindOllama, BaseURL: fmt.Sprintf("http://127.0.0.1:%d", persisted.Providers.Ollama.Port+index)}
				byModel[model] = endpoint
				plan.OllamaRoutes = append(plan.OllamaRoutes, OllamaRoute{Model: model, Endpoint: endpoint})
				runtimeConfig.Topology.Endpoints = append(runtimeConfig.Topology.Endpoints, endpoint)
			}
			rewriteManagedAssignments(&runtimeConfig, managedOllamaEndpointID, byModel)
		}
	}

	mlxModels := managedMLXModels(runtimeConfig)
	if len(mlxModels) > 0 {
		base, _, found := managedEndpoint(runtimeConfig.Topology.Endpoints, managedMLXEndpointID)
		if !found || base.Kind != topology.KindOpenAICompatible {
			return RuntimePlan{}, fmt.Errorf("managed MLX endpoint is unavailable")
		}
		byModel := make(map[string]topology.Endpoint, len(mlxModels))
		for index, model := range mlxModels {
			endpoint := topology.Endpoint{ID: runtimeEndpointID("mlx", model), Name: "MLX · " + model, Kind: topology.KindOpenAICompatible, BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", persisted.Providers.MLX.Port+index)}
			byModel[model] = endpoint
			plan.MLXRoutes = append(plan.MLXRoutes, MLXRoute{Model: model, Endpoint: endpoint})
			runtimeConfig.Topology.Endpoints = append(runtimeConfig.Topology.Endpoints, endpoint)
		}
		rewriteManagedAssignments(&runtimeConfig, managedMLXEndpointID, byModel)
	}
	if err := runtimeConfig.Validate(); err != nil {
		return RuntimePlan{}, fmt.Errorf("validate runtime topology: %w", err)
	}
	plan.Config = runtimeConfig
	return plan, nil
}

func validateOllamaPortCapacity(c Config) error {
	if c.Providers.Ollama.PortMode == OllamaDedicatedPorts {
		count := len(managedOllamaModels(c))
		if count > 0 && c.Providers.Ollama.Port+count-1 > 65535 {
			return fmt.Errorf("Ollama does not have enough consecutive ports: needs %d starting at %d; choose a lower base port", count, c.Providers.Ollama.Port)
		}
	}
	return validateMLXPortCapacity(c)
}

func validateMLXPortCapacity(c Config) error {
	count := len(managedMLXModels(c))
	if count > 0 && c.Providers.MLX.Port+count-1 > 65535 {
		return fmt.Errorf("MLX does not have enough consecutive ports: needs %d starting at %d; choose a lower base port", count, c.Providers.MLX.Port)
	}
	return nil
}

func managedOllamaModels(c Config) []string {
	return managedModels(c, managedOllamaEndpointID, false)
}

func managedMLXModels(c Config) []string {
	return managedModels(c, managedMLXEndpointID, true)
}

func managedModels(c Config, endpointID string, sorted bool) []string {
	assignments := []topology.Assignment{c.Topology.Roles.King, c.Topology.Roles.Worker}
	if c.CouncilEnabled {
		assignments = append(assignments, c.Topology.Roles.Council)
	}
	seen := make(map[string]bool, len(assignments))
	models := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		model := strings.TrimSpace(assignment.Model)
		if assignment.EndpointID != endpointID || model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	if sorted {
		sort.Strings(models)
	}
	return models
}

func rewriteManagedAssignments(c *Config, sourceEndpointID string, endpoints map[string]topology.Endpoint) {
	assignments := []*topology.Assignment{&c.Topology.Roles.King, &c.Topology.Roles.Worker}
	if c.CouncilEnabled {
		assignments = append(assignments, &c.Topology.Roles.Council)
	}
	for _, assignment := range assignments {
		if assignment.EndpointID == sourceEndpointID {
			assignment.EndpointID = endpoints[strings.TrimSpace(assignment.Model)].ID
		}
	}
}

func managedEndpoint(endpoints []topology.Endpoint, endpointID string) (topology.Endpoint, int, bool) {
	for index, endpoint := range endpoints {
		if endpoint.ID == endpointID {
			return endpoint, index, true
		}
	}
	return topology.Endpoint{}, 0, false
}

func runtimeEndpointID(provider, model string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + strings.TrimSpace(model)))
	return fmt.Sprintf("%s-runtime-%x", provider, digest[:8])
}
