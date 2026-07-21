package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/modelcatalog"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type fakeModelSearcher struct {
	mu      sync.Mutex
	calls   []modelcatalog.Provider
	results map[modelcatalog.Provider][]modelcatalog.Model
	queries map[string][]modelcatalog.Model
}

type fakeModelDownloader struct {
	requests []localmodels.DownloadRequest
	err      error
}

func (f *fakeModelDownloader) Download(_ context.Context, request localmodels.DownloadRequest, report localmodels.DownloadReporter) error {
	f.requests = append(f.requests, request)
	report(localmodels.DownloadProgress{Model: request.Model, Status: "downloading", Percent: 40})
	return f.err
}

func (f *fakeModelSearcher) Search(_ context.Context, provider modelcatalog.Provider, query string, _ int) ([]modelcatalog.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, provider)
	if f.queries != nil {
		return append([]modelcatalog.Model(nil), f.queries[query]...), nil
	}
	return append([]modelcatalog.Model(nil), f.results[provider]...), nil
}

func TestModelsSearchCombinesEnabledProvidersAndRanksInstalledFirst(t *testing.T) {
	searcher := &fakeModelSearcher{results: map[modelcatalog.Provider][]modelcatalog.Model{
		modelcatalog.Ollama: {{Provider: modelcatalog.Ollama, ID: "qwen3:14b"}},
		modelcatalog.MLX:    {{Provider: modelcatalog.MLX, ID: "mlx-community/Qwen3-8B-4bit"}},
	}}
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{
		{Kind: localmodels.KindOllama, Models: []localmodels.Model{{ID: "qwen3:8b"}}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"}},
		{Kind: localmodels.KindMLX, Models: []localmodels.Model{{ID: "mlx-community/Qwen3-4B-4bit"}}, Endpoint: topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX"}},
	}}
	m := modelAtModelsScreen(t, manager, searcher)

	m, _ = update(m, key("/"))
	m, search := update(m, key("q"))
	if search == nil || !strings.Contains(m.View().Content, "Searching Ollama and MLX") {
		t.Fatalf("search did not start: %s", m.View().Content)
	}
	m, _ = update(m, search())
	view := m.View().Content
	for _, want := range []string{"qwen3:8b", "mlx-community/Qwen3-4B-4bit", "qwen3:14b", "mlx-community/Qwen3-8B-4bit", "Installed", "Download"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search view missing %q: %s", want, view)
		}
	}
	if strings.Index(view, "qwen3:8b") > strings.Index(view, "qwen3:14b") {
		t.Fatalf("installed result ranked after online result: %s", view)
	}
	searcher.mu.Lock()
	defer searcher.mu.Unlock()
	if len(searcher.calls) != 2 {
		t.Fatalf("providers searched=%v", searcher.calls)
	}
}

func TestModelsSearchIgnoresResultsFromAnOlderQuery(t *testing.T) {
	searcher := &fakeModelSearcher{queries: map[string][]modelcatalog.Model{
		"q":  {{Provider: modelcatalog.Ollama, ID: "obsolete-q-result"}},
		"qw": {{Provider: modelcatalog.Ollama, ID: "current-qw-result"}},
	}}
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{{
		Kind: localmodels.KindOllama, Models: []localmodels.Model{{ID: "qwen3:8b"}}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
	}}}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager, ModelSearch: searcher})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0]}})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m, inventory := update(m, key("enter"))
	m, _ = update(m, inventory())
	m, _ = update(m, key("/"))
	m, oldSearch := update(m, key("q"))
	m, currentSearch := update(m, key("w"))

	m, _ = update(m, oldSearch())
	if strings.Contains(m.View().Content, "obsolete-q-result") {
		t.Fatalf("stale search result replaced current query: %s", m.View().Content)
	}
	m, _ = update(m, currentSearch())
	if !strings.Contains(m.View().Content, "current-qw-result") {
		t.Fatalf("current search result missing: %s", m.View().Content)
	}
}

func TestContinuingWithOnlineModelsRequiresDownloadConfirmation(t *testing.T) {
	searcher := &fakeModelSearcher{results: map[modelcatalog.Provider][]modelcatalog.Model{
		modelcatalog.Ollama: {{Provider: modelcatalog.Ollama, ID: "qwen3:14b"}},
	}}
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{{
		Kind: localmodels.KindOllama, Models: []localmodels.Model{{ID: "qwen3:8b"}}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama"},
	}}}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager, ModelSearch: searcher, ModelDownload: &fakeModelDownloader{}})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0]}})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m, inventory := update(m, key("enter"))
	m, _ = update(m, inventory())
	m, _ = update(m, key("/"))
	m, search := update(m, key("q"))
	m, _ = update(m, search())
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter")) // finish search
	m, _ = update(m, key("enter")) // continue from Models

	view := m.View().Content
	if m.screen != setup.StateModels || !strings.Contains(view, "Download selected model?") || !strings.Contains(view, "qwen3:14b") {
		t.Fatalf("missing download confirmation: screen=%v view=%s", m.screen, view)
	}
	m, _ = update(m, key("n"))
	if strings.Contains(m.View().Content, "Download selected model?") {
		t.Fatalf("download confirmation did not close: %s", m.View().Content)
	}
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("y"))
	if m.screen != setup.StateRoles {
		t.Fatalf("confirmed selection did not advance: screen=%v", m.screen)
	}
}

func TestConfirmedDownloadStartsInBackgroundAndContinuesToRoles(t *testing.T) {
	downloader := &fakeModelDownloader{}
	searcher := &fakeModelSearcher{results: map[modelcatalog.Provider][]modelcatalog.Model{
		modelcatalog.Ollama: {{Provider: modelcatalog.Ollama, ID: "qwen3:14b"}},
	}}
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{{
		Kind: localmodels.KindOllama, Models: []localmodels.Model{{ID: "qwen3:8b"}}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama", BaseURL: "http://127.0.0.1:11434"},
	}}}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager, ModelSearch: searcher, ModelDownload: downloader})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0]}})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m, inventory := update(m, key("enter"))
	m, _ = update(m, inventory())
	m, _ = update(m, key("/"))
	m, search := update(m, key("q"))
	m, _ = update(m, search())
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("enter"))
	m, command := update(m, key("y"))
	if m.screen != setup.StateRoles || command == nil || !strings.Contains(m.View().Content, "Preparing model download") {
		t.Fatalf("download did not continue to roles: screen=%v view=%s", m.screen, m.View().Content)
	}
	for command != nil {
		m, command = update(m, command())
	}
	if len(downloader.requests) != 1 || downloader.requests[0].Model != "qwen3:14b" || len(m.workflow.Draft.PendingDownloads()) != 0 {
		t.Fatalf("requests=%+v pending=%+v", downloader.requests, m.workflow.Draft.PendingDownloads())
	}
}

func TestReviewCannotSaveWhileRequiredDownloadsArePendingOrFailed(t *testing.T) {
	saves := 0
	m := NewWithServices(config.Default(), Services{Save: func(config.Config) error {
		saves++
		return nil
	}})
	m.setup = true
	m.screen = setup.StateReview
	m.workflow.State = setup.StateReview
	m.modelDownloadActive = true

	m, command := update(m, key("enter"))
	if command != nil || saves != 0 || !strings.Contains(m.workflow.Err.Error(), "still in progress") {
		t.Fatalf("active download reached save: command=%v saves=%d err=%v", command, saves, m.workflow.Err)
	}
	m.modelDownloadActive = false
	m.modelDownloadError = "network unavailable"
	m, command = update(m, key("enter"))
	if command != nil || saves != 0 || !strings.Contains(m.workflow.Err.Error(), "network unavailable") {
		t.Fatalf("failed download reached save: command=%v saves=%d err=%v", command, saves, m.workflow.Err)
	}
}

func modelAtModelsScreen(t *testing.T, manager LocalModelManager, searcher ModelSearcher) Model {
	t.Helper()
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager, ModelSearch: searcher})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0]}, {Endpoint: discovery.DefaultEndpoints()[1]}})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.Config.Providers.MLX.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m.workflow.Draft.SetProviderReady(setup.MLXEndpointID, true)
	m, inventory := update(m, key("enter"))
	if inventory == nil {
		t.Fatal("inventory command is nil")
	}
	m, _ = update(m, inventory())
	return m
}

func TestModelsScreenLoadsInstalledInventoryAcrossEnabledProviders(t *testing.T) {
	manager := &fakeLocalModelManager{runtimes: []localmodels.Runtime{
		{
			Kind:      localmodels.KindOllama,
			Name:      "Ollama",
			Installed: true,
			Running:   true,
			Models:    []localmodels.Model{{ID: "qwen3:8b", Loaded: true}},
			Endpoint:  topology.Endpoint{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"},
		},
		{
			Kind:      localmodels.KindMLX,
			Name:      "MLX",
			Installed: true,
			Models:    []localmodels.Model{{ID: "mlx-community/Qwen3-4B-4bit"}, {ID: "mlx-community/Mistral-7B-4bit"}},
			Endpoint:  topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"},
		},
	}}
	m := NewWithServices(config.Default(), Services{Defaults: discovery.DefaultEndpoints(), LocalModels: manager})
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: discovery.DefaultEndpoints()[0]},
		{Endpoint: discovery.DefaultEndpoints()[1]},
	})
	m.workflow.Draft.Config.Providers.Ollama.Enabled = true
	m.workflow.Draft.Config.Providers.MLX.Enabled = true
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m.workflow.Draft.SetProviderReady(setup.MLXEndpointID, true)

	m, command := update(m, key("enter"))
	if m.screen != setup.StateModels || command == nil {
		t.Fatalf("models inventory did not start: screen=%v command=%v", m.screen, command)
	}
	if view := m.View().Content; !strings.Contains(view, "Checking installed models") {
		t.Fatalf("missing inventory loading state: %s", view)
	}

	m, _ = update(m, command())
	view := m.View().Content
	for _, want := range []string{"qwen3:8b", "mlx-community/Qwen3-4B-4bit", "mlx-community/Mistral-7B-4bit", "Ollama", "MLX"} {
		if !strings.Contains(view, want) {
			t.Fatalf("combined inventory missing %q: %s", want, view)
		}
	}
}

func TestModelsScreenSelectsAcrossProviders(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(crossProviderResults())

	_ = m.workflow.Draft.SetProviderEnabled(setup.OllamaEndpointID, true, setup.Platform{OS: "linux", Arch: "amd64"})
	m, _ = update(m, key("enter")) // providers -> models
	if m.screen != setup.StateModels {
		t.Fatalf("screen=%v, want models", m.screen)
	}
	m, _ = update(m, key(" "))
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	selected := m.workflow.Draft.SelectedModels()
	if len(selected) != 2 || selected[0].Ref.EndpointID != "ollama-local" || selected[1].Ref.EndpointID != "mlx-local" {
		t.Fatalf("selected=%+v", selected)
	}
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateRoles {
		t.Fatalf("screen=%v, want roles", m.screen)
	}
}

func TestProviderScreenRequiresAnExplicitProviderChoice(t *testing.T) {
	m := New(config.Default())
	m.workflow.Draft.ApplyResults(onboardingResults())
	m, _ = update(m, key("enter"))
	view := m.View().Content
	if m.screen != setup.StateProviders || !strings.Contains(view, "enable at least one provider") {
		t.Fatalf("provider screen advanced without a choice: %s", view)
	}
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	view = m.View().Content
	for _, want := range []string{"ollama-model", "mlx-model-a", "mlx-model-b"} {
		if !strings.Contains(view, want) {
			t.Fatalf("models view missing %q: %s", want, view)
		}
	}
}

func crossProviderResults() []setup.EndpointResult {
	return []setup.EndpointResult{
		{Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"}, Models: []discovery.Model{{ID: "ollama-small", ParameterSize: "3B"}}},
		{Endpoint: topology.Endpoint{ID: "mlx-local", Name: "MLX"}, Models: []discovery.Model{{ID: "mlx-large", ParameterSize: "14B"}}},
	}
}
