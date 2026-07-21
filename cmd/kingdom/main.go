package main

import (
	"context"
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
		Run: func(ctx context.Context, cfg config.Config, prompt string, active []skills.Skill) <-chan orchestration.Event {
			return orchestration.NewEngine(cfg, client, orchestration.WithTools(toolRunner), orchestration.WithSkills(active), orchestration.WithMemory(memoryStore, sessionID, 6)).Stream(ctx, prompt)
		},
		Skills:      skillLibrary,
		Memory:      memoryStore,
		LocalModels: localModelManager,
		Installer:   providerInstaller,
		ModelSearch: modelcatalog.DefaultRemote(nil),
	}
	m := app.NewWithServices(c, services)
	program := tea.NewProgram(m)
	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}
