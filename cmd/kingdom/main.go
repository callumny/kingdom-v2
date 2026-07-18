package main

import (
	"context"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/app"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/setup"
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
	client := modelapi.NewClient()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	toolRunner, err := tools.NewRunner(workspace)
	if err != nil {
		log.Fatal(err)
	}
	m := app.NewWithServices(c, discovery.DefaultEndpoints(), func(ctx context.Context, gen uint64, candidates []topology.Endpoint) tea.Cmd {
		return func() tea.Msg {
			rs, _ := d.Discover(ctx, candidates)
			out := make([]setup.EndpointResult, len(rs))
			for i, r := range rs {
				out[i] = setup.EndpointResult{Endpoint: r.Endpoint, Models: r.Models, Err: r.Err}
			}
			return app.DiscoveryMsg{Generation: gen, Results: out}
		}
	}, func(next config.Config) error { return config.Save(path, next) }, func(ctx context.Context, cfg config.Config, prompt string) <-chan orchestration.Event {
		return orchestration.NewEngineWithTools(cfg, client, toolRunner).Stream(ctx, prompt)
	})
	program := tea.NewProgram(m)
	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}
