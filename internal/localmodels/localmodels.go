// Package localmodels discovers and starts already-installed local model runtimes.
package localmodels

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

type Kind string

const (
	KindOllama Kind = "ollama"
	KindMLX    Kind = "mlx"
)

type Model struct {
	ID            string
	LocalPath     string
	Loaded        bool
	SizeBytes     int64
	Family        string
	ParameterSize string
	Quantization  string
}

type Runtime struct {
	Kind        Kind
	Name        string
	Installed   bool
	Running     bool
	Models      []Model
	Endpoint    topology.Endpoint
	Warning     string
	InstallHint string
}

// ModelServer binds one local model to one dedicated provider endpoint.
type ModelServer struct {
	Model    string
	Endpoint topology.Endpoint
}

type System interface {
	LookPath(string) (string, error)
	Output(context.Context, string, ...string) ([]byte, error)
	Start(string, []string, []string) error
}

type Discoverer interface {
	Discover(context.Context, []topology.Endpoint) ([]discovery.Result, error)
}

type provider interface {
	inspect(context.Context) Runtime
	start(context.Context, string) error
	kind() Kind
}

type Manager struct{ providers []provider }

func New(system System, discoverer Discoverer, mlxCacheRoot string) *Manager {
	return NewWithRuntimeRoot(system, discoverer, mlxCacheRoot, "")
}

func NewWithRuntimeRoot(system System, discoverer Discoverer, mlxCacheRoot, runtimeRoot string) *Manager {
	endpoints := discovery.DefaultEndpoints()
	byID := make(map[string]topology.Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
	}
	mlxExecutable := ""
	if runtimeRoot != "" {
		mlxExecutable = filepath.Join(runtimeRoot, "mlx", "bin", "mlx_lm.server")
	}
	return &Manager{providers: []provider{
		&ollamaProvider{system: system, discoverer: discoverer, endpoint: byID["ollama-local"]},
		&mlxProvider{system: system, discoverer: discoverer, endpoint: byID["mlx-local"], cacheRoot: mlxCacheRoot, executable: mlxExecutable},
	}}
}

func (m *Manager) Inspect(ctx context.Context) []Runtime {
	if m == nil {
		return nil
	}
	runtimes := make([]Runtime, len(m.providers))
	done := make(chan int, len(m.providers))
	for index, adapter := range m.providers {
		go func(index int, adapter provider) {
			if ctx.Err() != nil {
				runtimes[index] = Runtime{Kind: adapter.kind(), Warning: ctx.Err().Error()}
			} else {
				runtimes[index] = adapter.inspect(ctx)
			}
			done <- index
		}(index, adapter)
	}
	for range m.providers {
		<-done
	}
	return runtimes
}

func (m *Manager) Start(ctx context.Context, kind Kind, modelID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, provider := range m.providers {
		if provider.kind() == kind {
			return provider.start(ctx, strings.TrimSpace(modelID))
		}
	}
	return fmt.Errorf("unknown local runtime %q", kind)
}

// ConfigureAndStart applies provider-level network configuration before
// launch. MLX is model-scoped and is therefore started only after selection.
func (m *Manager) ConfigureAndStart(ctx context.Context, kind Kind, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("provider port must be 1..65535")
	}
	if kind != KindOllama {
		return fmt.Errorf("provider %q requires a model before startup", kind)
	}
	for _, candidate := range m.providers {
		provider, ok := candidate.(*ollamaProvider)
		if !ok {
			continue
		}
		provider.endpoint.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
		return m.StartAndWait(ctx, kind, "")
	}
	return errors.New("Ollama provider is unavailable")
}

// EnsureOllamaServers starts each missing loopback Ollama endpoint and waits
// for it to accept requests. Endpoints sharing a host and port are started
// only once.
func (m *Manager) EnsureOllamaServers(ctx context.Context, endpoints []topology.Endpoint) error {
	if len(endpoints) == 0 {
		return nil
	}
	if m == nil {
		return errors.New("local model manager is unavailable")
	}
	var base *ollamaProvider
	for _, candidate := range m.providers {
		if provider, ok := candidate.(*ollamaProvider); ok {
			base = provider
			break
		}
	}
	if base == nil {
		return errors.New("Ollama provider is unavailable")
	}

	waitContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	seen := make(map[string]bool, len(endpoints))
	for _, endpoint := range endpoints {
		host, err := validateOllamaServerEndpoint(endpoint)
		if err != nil {
			return fmt.Errorf("Ollama endpoint %q: %w", endpoint.ID, err)
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		if err := waitContext.Err(); err != nil {
			return err
		}
		running, _, _ := probe(waitContext, base.discoverer, endpoint)
		if running {
			continue
		}
		target := *base
		target.endpoint = endpoint
		if err := target.start(waitContext, ""); err != nil {
			return fmt.Errorf("start Ollama on %s: %w", host, err)
		}
		if err := waitForProvider(waitContext, &target, KindOllama, ""); err != nil {
			return fmt.Errorf("Ollama on %s: %w", host, err)
		}
	}
	return nil
}

// EnsureMLXServers starts one MLX server per model and waits for each model to
// answer on its assigned loopback endpoint.
func (m *Manager) EnsureMLXServers(ctx context.Context, servers []ModelServer) error {
	if len(servers) == 0 {
		return nil
	}
	if m == nil {
		return errors.New("local model manager is unavailable")
	}
	var base *mlxProvider
	for _, candidate := range m.providers {
		if provider, ok := candidate.(*mlxProvider); ok {
			base = provider
			break
		}
	}
	if base == nil {
		return errors.New("MLX provider is unavailable")
	}

	waitContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	byAddress := make(map[string]string, len(servers))
	for _, server := range servers {
		modelID := strings.TrimSpace(server.Model)
		if modelID == "" {
			return errors.New("MLX server requires a model")
		}
		address, _, err := validateMLXServerEndpoint(server.Endpoint)
		if err != nil {
			return fmt.Errorf("MLX endpoint %q: %w", server.Endpoint.ID, err)
		}
		if existing := byAddress[address]; existing != "" && existing != modelID {
			return fmt.Errorf("MLX endpoint %s is assigned to both %q and %q", address, existing, modelID)
		}
		byAddress[address] = modelID
		if err := waitContext.Err(); err != nil {
			return err
		}
		running, models, _ := probe(waitContext, base.discoverer, server.Endpoint)
		if running {
			servingSelectedModel := false
			for _, model := range models {
				if model.ID == modelID {
					servingSelectedModel = true
					break
				}
			}
			if servingSelectedModel {
				continue
			}
			return fmt.Errorf("MLX endpoint %s is already serving a different model", address)
		}
		target := *base
		target.endpoint = server.Endpoint
		if err := target.start(waitContext, modelID); err != nil {
			return fmt.Errorf("start MLX model %q on %s: %w", modelID, address, err)
		}
		if err := waitForProvider(waitContext, &target, KindMLX, modelID); err != nil {
			return fmt.Errorf("MLX model %q on %s: %w", modelID, address, err)
		}
	}
	return nil
}

func validateOllamaServerEndpoint(endpoint topology.Endpoint) (string, error) {
	if endpoint.Kind != topology.KindOllama {
		return "", errors.New("must use the Ollama endpoint kind")
	}
	parsed, err := url.Parse(endpoint.BaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.Port() == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("must be an HTTP loopback URL with an explicit port")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("port must be 1..65535")
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		return "", errors.New("must bind to localhost")
	}
	return parsed.Host, nil
}

func (m *Manager) StartAndWait(ctx context.Context, kind Kind, modelID string) error {
	waitContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var target provider
	for _, candidate := range m.providers {
		if candidate.kind() == kind {
			target = candidate
			break
		}
	}
	if target == nil {
		return fmt.Errorf("unknown local runtime %q", kind)
	}
	if err := target.start(waitContext, strings.TrimSpace(modelID)); err != nil {
		return err
	}
	return waitForProvider(waitContext, target, kind, modelID)
}

func waitForProvider(waitContext context.Context, target provider, kind Kind, modelID string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		runtime := target.inspect(waitContext)
		if runtime.Running {
			if modelID == "" || kind == KindOllama {
				return nil
			}
			for _, model := range runtime.Models {
				if model.ID == modelID && model.Loaded {
					return nil
				}
			}
		}
		select {
		case <-waitContext.Done():
			return fmt.Errorf("wait for %s readiness: %w", kind, waitContext.Err())
		case <-ticker.C:
		}
	}
}

func probe(ctx context.Context, discoverer Discoverer, endpoint topology.Endpoint) (bool, []discovery.Model, error) {
	if discoverer == nil {
		return false, nil, errors.New("model discovery unavailable")
	}
	results, err := discoverer.Discover(ctx, []topology.Endpoint{endpoint})
	if err != nil {
		return false, nil, err
	}
	if len(results) != 1 {
		return false, nil, errors.New("model discovery returned no result")
	}
	if results[0].Err != nil {
		return false, nil, results[0].Err
	}
	models := make([]discovery.Model, 0, len(results[0].Models))
	for _, model := range results[0].Models {
		if id := strings.TrimSpace(model.ID); id != "" {
			model.ID = id
			models = append(models, model)
		}
	}
	return true, models, nil
}

func discoveredModelIDs(models []discovery.Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func normalizedModels(models []Model) []Model {
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			continue
		}
		if current, exists := byID[model.ID]; !exists || (!current.Loaded && model.Loaded) {
			byID[model.ID] = model
		}
	}
	result := make([]Model, 0, len(byID))
	for _, model := range byID {
		result = append(result, model)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].ID), strings.ToLower(result[j].ID)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result
}

func markLoaded(models []Model, loaded []string) []Model {
	set := make(map[string]bool, len(loaded))
	for _, id := range loaded {
		set[id] = true
	}
	for index := range models {
		models[index].Loaded = set[models[index].ID]
	}
	return models
}

func combineWarnings(values ...error) string {
	var messages []string
	for _, err := range values {
		if err != nil && !errors.Is(err, context.Canceled) {
			messages = append(messages, err.Error())
		}
	}
	return strings.Join(messages, "; ")
}
