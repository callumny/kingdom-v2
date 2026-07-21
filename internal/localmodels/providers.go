package localmodels

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

type ollamaProvider struct {
	system     System
	discoverer Discoverer
	endpoint   topology.Endpoint
}

func (*ollamaProvider) kind() Kind { return KindOllama }

func (p *ollamaProvider) inspect(ctx context.Context) Runtime {
	result := Runtime{Kind: p.kind(), Name: "Ollama", Endpoint: p.endpoint, InstallHint: "Install from https://ollama.com/download"}
	executable, err := p.system.LookPath("ollama")
	if err != nil || executable == "" {
		return result
	}
	result.Installed = true
	running, models, probeErr := probe(ctx, p.discoverer, p.endpoint)
	result.Running = running
	for _, model := range models {
		result.Models = append(result.Models, Model{
			ID:            model.ID,
			Loaded:        true,
			SizeBytes:     model.SizeBytes,
			Family:        model.Family,
			ParameterSize: model.ParameterSize,
			Quantization:  model.Quantization,
		})
	}
	result.Models = normalizedModels(result.Models)
	result.Warning = combineWarnings(probeErr)
	return result
}

func (p *ollamaProvider) start(_ context.Context, modelID string) error {
	if modelID != "" {
		return errors.New("Ollama service start does not accept a model")
	}
	executable, err := p.system.LookPath("ollama")
	if err != nil {
		return errors.New("Ollama CLI is not installed")
	}
	host := "127.0.0.1:11434"
	if parsed, err := url.Parse(p.endpoint.BaseURL); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	if err := p.system.Start(executable, []string{"serve"}, []string{"OLLAMA_HOST=" + host}); err != nil {
		return fmt.Errorf("start Ollama: %w", err)
	}
	return nil
}

type mlxProvider struct {
	system     System
	discoverer Discoverer
	endpoint   topology.Endpoint
	cacheRoot  string
	executable string
}

func (*mlxProvider) kind() Kind { return KindMLX }

func (p *mlxProvider) inspect(ctx context.Context) Runtime {
	result := Runtime{Kind: p.kind(), Name: "MLX", Endpoint: p.endpoint, InstallHint: "On Apple silicon, install the mlx-lm Python package"}
	executable, err := p.serverExecutable()
	if err != nil || executable == "" {
		return result
	}
	result.Installed = true
	models, listErr := scanMLXCache(p.cacheRoot)
	running, loaded, probeErr := probe(ctx, p.discoverer, p.endpoint)
	result.Running = running
	result.Models = normalizedModels(markLoaded(models, discoveredModelIDs(loaded)))
	result.Warning = combineWarnings(listErr, probeErr)
	return result
}

func (p *mlxProvider) start(_ context.Context, modelID string) error {
	if modelID == "" {
		return errors.New("select an installed MLX model")
	}
	executable, err := p.serverExecutable()
	if err != nil {
		return errors.New("MLX LM server is not installed")
	}
	models, err := scanMLXCache(p.cacheRoot)
	if err != nil {
		return err
	}
	if !containsModel(models, modelID) {
		return fmt.Errorf("MLX model %q is not installed in the local cache", modelID)
	}
	args := []string{"--model", modelID, "--host", "127.0.0.1", "--port", "8080"}
	if err := p.system.Start(executable, args, []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=" + p.cacheRoot}); err != nil {
		return fmt.Errorf("start MLX model: %w", err)
	}
	return nil
}

func (p *mlxProvider) serverExecutable() (string, error) {
	if p.executable != "" {
		if info, err := os.Stat(p.executable); err == nil && !info.IsDir() {
			return p.executable, nil
		}
	}
	return p.system.LookPath("mlx_lm.server")
}

func containsModel(models []Model, id string) bool {
	for _, model := range models {
		if model.ID == id {
			return true
		}
	}
	return false
}

func scanMLXCache(root string) ([]Model, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil, errors.New("MLX cache directory is unavailable")
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MLX model cache: %w", err)
	}
	var models []Model
	for index, entry := range entries {
		if index >= 256 || !entry.IsDir() || !strings.HasPrefix(entry.Name(), "models--") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(entry.Name(), "models--"), "--")
		if len(parts) < 2 || parts[0] == "" || strings.Join(parts[1:], "--") == "" {
			continue
		}
		modelID := parts[0] + "/" + strings.Join(parts[1:], "--")
		snapshotsRoot := filepath.Join(root, entry.Name(), "snapshots")
		snapshots, readErr := os.ReadDir(snapshotsRoot)
		if readErr != nil {
			continue
		}
		for snapshotIndex := len(snapshots) - 1; snapshotIndex >= 0; snapshotIndex-- {
			snapshot := snapshots[snapshotIndex]
			if !snapshot.IsDir() {
				continue
			}
			path := filepath.Join(snapshotsRoot, snapshot.Name())
			if completeMLXSnapshot(path) {
				models = append(models, Model{ID: modelID, LocalPath: path})
				break
			}
		}
	}
	return normalizedModels(models), nil
}

func completeMLXSnapshot(path string) bool {
	if info, err := os.Stat(filepath.Join(path, "config.json")); err != nil || info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".safetensors") || strings.HasSuffix(entry.Name(), ".safetensors.index.json")) {
			return true
		}
	}
	return false
}

func DefaultMLXCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve MLX cache directory: %w", err)
	}
	return filepath.Join(home, ".cache", "huggingface", "hub"), nil
}
