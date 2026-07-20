// Package localmodels discovers and starts already-installed local model runtimes.
package localmodels

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	ID        string
	LocalPath string
	Loaded    bool
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
	endpoints := discovery.DefaultEndpoints()
	byID := make(map[string]topology.Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
	}
	return &Manager{providers: []provider{
		&ollamaProvider{system: system, discoverer: discoverer, endpoint: byID["ollama-local"]},
		&mlxProvider{system: system, discoverer: discoverer, endpoint: byID["mlx-local"], cacheRoot: mlxCacheRoot},
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

func probe(ctx context.Context, discoverer Discoverer, endpoint topology.Endpoint) (bool, []string, error) {
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
	ids := make([]string, 0, len(results[0].Models))
	for _, model := range results[0].Models {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return true, ids, nil
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
