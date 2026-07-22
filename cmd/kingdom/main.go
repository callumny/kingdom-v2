package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

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
	"github.com/callumny/kingdom/internal/wizard"
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
		Run: func(ctx context.Context, cfg config.Config, prompt string, active []skills.Skill) <-chan orchestration.Event {
			return orchestration.NewEngine(cfg, client, orchestration.WithTools(toolRunner), orchestration.WithSkills(active), orchestration.WithMemory(memoryStore, sessionID, 6)).Stream(ctx, prompt)
		},
		Skills:        skillLibrary,
		Memory:        memoryStore,
		LocalModels:   localModelManager,
		Installer:     providerInstaller,
		ModelSearch:   modelcatalog.DefaultRemote(nil),
		ModelDownload: modelDownloader,
		WizardBenchmark: wizard.Benchmarker{
			Client:          client,
			TimeoutPerModel: 30 * time.Second,
			Prepare: func(ctx context.Context, models []setup.ModelOption) []wizard.PreparedModel {
				return prepareWizardModels(ctx, c, models, localModelManager.EnsureOllamaServers, localModelManager.EnsureMLXServers)
			},
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

func prepareWizardModels(
	ctx context.Context,
	cfg config.Config,
	models []setup.ModelOption,
	ensureOllama func(context.Context, []topology.Endpoint) error,
	ensureMLX func(context.Context, []localmodels.ModelServer) error,
) []wizard.PreparedModel {
	prepared := make([]wizard.PreparedModel, len(models))
	var ollamaIndexes []int
	mlxIndexes := make(map[string][]int)
	for index, model := range models {
		prepared[index].Model = model
		switch model.Ref.EndpointID {
		case setup.OllamaEndpointID:
			prepared[index].Model.Endpoint = topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: fmt.Sprintf("http://127.0.0.1:%d", cfg.Providers.Ollama.Port)}
			ollamaIndexes = append(ollamaIndexes, index)
		case setup.MLXEndpointID:
			mlxIndexes[model.Ref.ModelID] = append(mlxIndexes[model.Ref.ModelID], index)
		default:
			prepared[index].Err = fmt.Errorf("unsupported Wizard provider %q", model.Ref.EndpointID)
		}
	}
	if len(ollamaIndexes) > 0 {
		if ensureOllama == nil {
			for _, index := range ollamaIndexes {
				prepared[index].Err = fmt.Errorf("Ollama runtime manager is unavailable")
			}
		} else if err := ensureOllama(ctx, []topology.Endpoint{prepared[ollamaIndexes[0]].Model.Endpoint}); err != nil {
			for _, index := range ollamaIndexes {
				prepared[index].Err = err
			}
		}
	}
	mlxModels := make([]string, 0, len(mlxIndexes))
	for model := range mlxIndexes {
		mlxModels = append(mlxModels, model)
	}
	sort.Strings(mlxModels)
	benchmarkBase := cfg.Providers.MLX.Port + setup.MaxSelectedModels
	for offset, model := range mlxModels {
		port := benchmarkBase + offset
		endpoint := topology.Endpoint{ID: fmt.Sprintf("mlx-benchmark-%d", offset), Name: "MLX · " + model, Kind: topology.KindOpenAICompatible, BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", port)}
		for _, index := range mlxIndexes[model] {
			prepared[index].Model.Endpoint = endpoint
		}
		var err error
		if port > 65535 {
			err = fmt.Errorf("MLX benchmark port exceeds 65535; choose a lower MLX base port")
		} else if ensureMLX == nil {
			err = fmt.Errorf("MLX runtime manager is unavailable")
		} else {
			err = ensureMLX(ctx, []localmodels.ModelServer{{Model: model, Endpoint: endpoint}})
		}
		if err != nil {
			for _, index := range mlxIndexes[model] {
				prepared[index].Err = err
			}
		}
	}
	return prepared
}
