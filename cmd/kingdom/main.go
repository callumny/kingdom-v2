package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/app"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/modelcatalog"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
)

func main() {
	path, err := config.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	d := discovery.New(discovery.DefaultOptions())
	mlxCacheRoot, err := localmodels.DefaultMLXCacheRoot()
	if err != nil {
		log.Fatal(err)
	}
	runtimeRoot := filepath.Join(filepath.Dir(path), "runtimes")
	localModelManager := localmodels.NewWithRuntimeRoot(localmodels.OSSystem{}, d, mlxCacheRoot, runtimeRoot)
	providerInstaller := localmodels.NewInstaller(localmodels.OSSystem{}, runtimeRoot)
	modelDownloader := localmodels.NewDownloader(localmodels.OSSystem{}, nil, runtimeRoot, mlxCacheRoot)
	client := modelapi.NewClient()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	toolRunner, err := tools.NewRunner(workspace)
	if err != nil {
		log.Fatal(err)
	}
	skillLibrary := skills.NewLibrary(filepath.Join(filepath.Dir(path), "skills"), skills.DefaultBuiltIns())
	if err := skillLibrary.EnsureDir(); err != nil {
		log.Fatal(err)
	}
	memoryStore, err := memory.Open(filepath.Join(filepath.Dir(path), "memory.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer memoryStore.Close()
	sessionID, err := memory.NewSessionID()
	if err != nil {
		log.Fatal(err)
	}
	services := app.Services{
		Defaults: discovery.DefaultEndpoints(),
		Discover: func(ctx context.Context, gen uint64, candidates []topology.Endpoint) tea.Cmd {
			return func() tea.Msg {
				results, _ := d.Discover(ctx, candidates)
				out := make([]setup.EndpointResult, len(results))
				for index, result := range results {
					out[index] = setup.EndpointResult{Endpoint: result.Endpoint, Models: result.Models, Err: result.Err}
				}
				return app.DiscoveryMsg{Generation: gen, Results: out}
			}
		},
		Save: func(next config.Config) error { return config.Save(path, next) },
		PrepareRun: func(ctx context.Context, cfg config.Config) (config.Config, error) {
			return prepareRuntimeConfig(ctx, cfg, localModelManager.EnsureOllamaServers, localModelManager.EnsureMLXServers)
		},
		WarmRun: func(ctx context.Context, cfg config.Config) (config.Config, error) {
			return warmRuntimeConfig(ctx, cfg, localModelManager.EnsureOllamaServers, localModelManager.EnsureMLXServers, client.PreloadOllama)
		},
		Run: func(ctx context.Context, cfg config.Config, prompt string, active []skills.Skill) <-chan orchestration.Event {
			return orchestration.NewEngine(cfg, client, orchestration.WithTools(toolRunner), orchestration.WithSkills(active), orchestration.WithMemory(memoryStore, sessionID, 6)).Stream(ctx, prompt)
		},
		Skills:        skillLibrary,
		Memory:        memoryStore,
		LocalModels:   localModelManager,
		Installer:     providerInstaller,
		ModelSearch:   modelcatalog.DefaultRemote(nil),
		ModelDownload: modelDownloader,
		ModelRemove:   modelDownloader,
		PrepareWizard: func(ctx context.Context, cfg config.Config, model setup.ModelOption) (setup.ModelOption, error) {
			return prepareWizardModel(ctx, cfg, model, localModelManager.EnsureOllamaServers, localModelManager.EnsureMLXServers)
		},
		WizardClient: client,
	}
	m := app.NewWithServices(c, services)
	program := tea.NewProgram(m)
	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}

func prepareRuntimeConfig(
	ctx context.Context,
	persisted config.Config,
	ensureOllama func(context.Context, []topology.Endpoint) error,
	ensureMLX func(context.Context, []localmodels.ModelServer) error,
) (config.Config, error) {
	plan, err := config.BuildRuntimePlan(persisted)
	if err != nil {
		return config.Config{}, fmt.Errorf("plan local runtimes: %w", err)
	}
	endpoints := make([]topology.Endpoint, 0, len(plan.OllamaRoutes))
	seen := make(map[string]bool, len(plan.OllamaRoutes))
	for _, route := range plan.OllamaRoutes {
		if seen[route.Endpoint.BaseURL] {
			continue
		}
		seen[route.Endpoint.BaseURL] = true
		endpoints = append(endpoints, route.Endpoint)
	}
	if len(endpoints) > 0 {
		if ensureOllama == nil {
			return config.Config{}, fmt.Errorf("prepare Ollama servers: local model manager is unavailable")
		}
		if err := ensureOllama(ctx, endpoints); err != nil {
			return config.Config{}, fmt.Errorf("prepare Ollama servers: %w", err)
		}
	}
	if len(plan.MLXRoutes) > 0 {
		if ensureMLX == nil {
			return config.Config{}, fmt.Errorf("prepare MLX servers: local model manager is unavailable")
		}
		servers := make([]localmodels.ModelServer, len(plan.MLXRoutes))
		for index, route := range plan.MLXRoutes {
			servers[index] = localmodels.ModelServer{Model: route.Model, Endpoint: route.Endpoint}
		}
		if err := ensureMLX(ctx, servers); err != nil {
			return config.Config{}, fmt.Errorf("prepare MLX servers: %w", err)
		}
	}
	return plan.Config, nil
}

func warmRuntimeConfig(
	ctx context.Context,
	persisted config.Config,
	ensureOllama func(context.Context, []topology.Endpoint) error,
	ensureMLX func(context.Context, []localmodels.ModelServer) error,
	preloadOllama func(context.Context, topology.Endpoint, string) error,
) (config.Config, error) {
	runtimeConfig, err := prepareRuntimeConfig(ctx, persisted, ensureOllama, ensureMLX)
	if err != nil {
		return config.Config{}, err
	}
	plan, err := config.BuildRuntimePlan(persisted)
	if err != nil {
		return config.Config{}, fmt.Errorf("plan warm-up: %w", err)
	}
	if len(plan.OllamaRoutes) > 0 && preloadOllama == nil {
		return config.Config{}, fmt.Errorf("preload Ollama models: model client is unavailable")
	}
	for _, route := range plan.OllamaRoutes {
		if err := preloadOllama(ctx, route.Endpoint, route.Model); err != nil {
			return config.Config{}, fmt.Errorf("preload Ollama model %q: %w", route.Model, err)
		}
	}
	return runtimeConfig, nil
}

func prepareWizardModel(
	ctx context.Context,
	cfg config.Config,
	model setup.ModelOption,
	ensureOllama func(context.Context, []topology.Endpoint) error,
	ensureMLX func(context.Context, []localmodels.ModelServer) error,
) (setup.ModelOption, error) {
	plan, err := config.BuildRuntimePlan(cfg)
	if err != nil {
		return model, fmt.Errorf("plan Wizard runtime: %w", err)
	}
	switch model.Ref.EndpointID {
	case setup.OllamaEndpointID:
		var found bool
		for _, route := range plan.OllamaRoutes {
			if route.Model == model.Ref.ModelID {
				model.Endpoint = route.Endpoint
				found = true
				break
			}
		}
		if !found {
			return model, fmt.Errorf("selected Ollama Wizard model is not in the runtime plan")
		}
		if ensureOllama == nil {
			return model, fmt.Errorf("Ollama runtime manager is unavailable")
		}
		if err := ensureOllama(ctx, []topology.Endpoint{model.Endpoint}); err != nil {
			return model, err
		}
		return model, nil
	case setup.MLXEndpointID:
		var found bool
		for _, route := range plan.MLXRoutes {
			if route.Model == model.Ref.ModelID {
				model.Endpoint = route.Endpoint
				found = true
				break
			}
		}
		if !found {
			return model, fmt.Errorf("selected MLX Wizard model is not in the runtime plan")
		}
		if ensureMLX == nil {
			return model, fmt.Errorf("MLX runtime manager is unavailable")
		}
		if err := ensureMLX(ctx, []localmodels.ModelServer{{Model: model.Ref.ModelID, Endpoint: model.Endpoint}}); err != nil {
			return model, err
		}
		return model, nil
	default:
		return model, fmt.Errorf("unsupported Wizard provider %q", model.Ref.EndpointID)
	}
}
