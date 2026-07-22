package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestPrepareRuntimeConfigStartsPlannedEndpoints(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaDedicatedPorts)
	var endpoints []topology.Endpoint
	runtimeConfig, err := prepareRuntimeConfig(context.Background(), cfg, func(_ context.Context, next []topology.Endpoint) error {
		endpoints = append([]topology.Endpoint(nil), next...)
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantURLs := []string{"http://127.0.0.1:12000", "http://127.0.0.1:12001"}
	gotURLs := make([]string, len(endpoints))
	for index := range endpoints {
		gotURLs[index] = endpoints[index].BaseURL
	}
	if !reflect.DeepEqual(gotURLs, wantURLs) {
		t.Fatalf("server URLs=%v want=%v", gotURLs, wantURLs)
	}
	if runtimeConfig.Topology.Roles.King.EndpointID == setup.OllamaEndpointID || runtimeConfig.Topology.Roles.Worker.EndpointID == setup.OllamaEndpointID {
		t.Fatalf("runtime roles were not routed: %+v", runtimeConfig.Topology.Roles)
	}
	if cfg.Topology.Roles.King.EndpointID != setup.OllamaEndpointID {
		t.Fatal("saved config was mutated")
	}
}

func TestPrepareRuntimeConfigDeduplicatesSharedEndpoint(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaSharedPort)
	var endpoints []topology.Endpoint
	_, err := prepareRuntimeConfig(context.Background(), cfg, func(_ context.Context, next []topology.Endpoint) error {
		endpoints = append([]topology.Endpoint(nil), next...)
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].BaseURL != "http://127.0.0.1:12000" {
		t.Fatalf("shared endpoints=%+v", endpoints)
	}
}

func TestPrepareRuntimeConfigReturnsStartupFailure(t *testing.T) {
	want := errors.New("start failed")
	_, err := prepareRuntimeConfig(context.Background(), managedRuntimeConfig(config.OllamaDedicatedPorts), func(context.Context, []topology.Endpoint) error {
		return want
	}, nil)
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
}

func TestPrepareRuntimeConfigStartsDedicatedMLXModels(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaSharedPort)
	cfg.Providers.MLX.Enabled = true
	cfg.Providers.MLX.Port = 13000
	cfg.Topology.Endpoints = append(cfg.Topology.Endpoints, topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:13000/v1"})
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.MLXEndpointID, Model: "large-mlx"}

	var servers []localmodels.ModelServer
	runtimeConfig, err := prepareRuntimeConfig(context.Background(), cfg, func(context.Context, []topology.Endpoint) error { return nil }, func(_ context.Context, next []localmodels.ModelServer) error {
		servers = append([]localmodels.ModelServer(nil), next...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Model != "large-mlx" || servers[0].Endpoint.BaseURL != "http://127.0.0.1:13000/v1" {
		t.Fatalf("MLX servers=%+v", servers)
	}
	if runtimeConfig.Topology.Roles.King.EndpointID == setup.MLXEndpointID {
		t.Fatalf("MLX role was not routed: %+v", runtimeConfig.Topology.Roles.King)
	}
}

func TestWarmRuntimeConfigPreloadsEveryRoutedOllamaModel(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaDedicatedPorts)
	var preloaded []string
	runtimeConfig, err := warmRuntimeConfig(
		context.Background(),
		cfg,
		func(context.Context, []topology.Endpoint) error { return nil },
		nil,
		func(_ context.Context, next topology.Endpoint, nextModel string) error {
			preloaded = append(preloaded, next.BaseURL+" "+nextModel)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://127.0.0.1:12000 large", "http://127.0.0.1:12001 small"}
	if !reflect.DeepEqual(preloaded, want) || runtimeConfig.Topology.Roles.King.EndpointID == setup.OllamaEndpointID {
		t.Fatalf("preloaded=%v roles=%+v", preloaded, runtimeConfig.Topology.Roles)
	}
}

func TestWarmRuntimeConfigPreloadsOnlyOllamaRoutesInMixedTopology(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaSharedPort)
	cfg.Providers.MLX.Enabled = true
	cfg.Providers.MLX.Port = 13000
	cfg.Topology.Endpoints = append(cfg.Topology.Endpoints, topology.Endpoint{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:13000/v1"})
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.MLXEndpointID, Model: "large-mlx"}
	var preloaded []string
	_, err := warmRuntimeConfig(
		context.Background(),
		cfg,
		func(context.Context, []topology.Endpoint) error { return nil },
		func(context.Context, []localmodels.ModelServer) error { return nil },
		func(_ context.Context, _ topology.Endpoint, model string) error {
			preloaded = append(preloaded, model)
			return nil
		},
	)
	if err != nil || !reflect.DeepEqual(preloaded, []string{"small", "large"}) {
		t.Fatalf("err=%v preloaded=%v", err, preloaded)
	}
}

func TestPrepareWizardModelReusesItsPlannedMLXPort(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.MLX.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.MLXEndpointID, Model: "small-model"}
	cfg.Topology.Roles.Worker = cfg.Topology.Roles.King
	cfg.Topology.Roles.Council = cfg.Topology.Roles.King
	model := setup.ModelOption{Ref: setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: "small-model"}, Endpoint: topology.Endpoint{ID: setup.MLXEndpointID}}
	var mlx []localmodels.ModelServer
	prepared, err := prepareWizardModel(context.Background(), cfg, model, nil,
		func(_ context.Context, servers []localmodels.ModelServer) error {
			mlx = append(mlx, servers...)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(mlx) != 1 || mlx[0].Model != "small-model" || mlx[0].Endpoint.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("MLX Wizard route=%+v", mlx)
	}
	if prepared.Endpoint != mlx[0].Endpoint {
		t.Fatalf("prepared endpoint=%+v server=%+v", prepared.Endpoint, mlx[0].Endpoint)
	}
}

func TestPrepareWizardModelReusesItsPlannedDedicatedOllamaPort(t *testing.T) {
	cfg := managedRuntimeConfig(config.OllamaDedicatedPorts)
	model := setup.ModelOption{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "small"}, Endpoint: topology.Endpoint{ID: setup.OllamaEndpointID}}
	var endpoints []topology.Endpoint
	prepared, err := prepareWizardModel(context.Background(), cfg, model,
		func(_ context.Context, next []topology.Endpoint) error {
			endpoints = append(endpoints, next...)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].BaseURL != "http://127.0.0.1:12001" || prepared.Endpoint != endpoints[0] {
		t.Fatalf("prepared=%+v endpoints=%+v", prepared, endpoints)
	}
}

func managedRuntimeConfig(mode config.OllamaPortMode) config.Config {
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.Ollama.Port = 12000
	cfg.Providers.Ollama.PortMode = mode
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12000"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "large"}
	cfg.Topology.Roles.Worker = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "small"}
	cfg.Topology.Roles.Council = cfg.Topology.Roles.King
	return cfg
}
