package setup

import (
	"fmt"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/topology"
)

const (
	OllamaEndpointID = "ollama-local"
	MLXEndpointID    = "mlx-local"
)

func ApplyProviderPorts(endpoints []topology.Endpoint, providers config.Providers) []topology.Endpoint {
	configured := append([]topology.Endpoint(nil), endpoints...)
	for index := range configured {
		switch configured[index].ID {
		case OllamaEndpointID:
			configured[index].BaseURL = fmt.Sprintf("http://127.0.0.1:%d", providers.Ollama.Port)
		case MLXEndpointID:
			configured[index].BaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", providers.MLX.Port)
		}
	}
	return configured
}

func (d Draft) ProviderEnabled(endpointID string) bool {
	switch endpointID {
	case OllamaEndpointID:
		return d.Config.Providers.Ollama.Enabled
	case MLXEndpointID:
		return d.Config.Providers.MLX.Enabled
	default:
		return false
	}
}

func (d Draft) ProviderReady(endpointID string) bool { return d.providerReady[endpointID] }

func (d *Draft) SetProviderReady(endpointID string, ready bool) {
	if d.providerReady == nil {
		d.providerReady = make(map[string]bool)
	}
	d.providerReady[endpointID] = ready
}

func (d Draft) ValidateEnabledProvidersReady() error {
	if d.Config.Providers.Ollama.Enabled && !d.ProviderReady(OllamaEndpointID) {
		return fmt.Errorf("Ollama must finish setup before continuing")
	}
	if d.Config.Providers.MLX.Enabled && !d.ProviderReady(MLXEndpointID) {
		return fmt.Errorf("MLX must finish setup before continuing")
	}
	return nil
}

func (d *Draft) SetProviderEnabled(endpointID string, enabled bool, platform Platform) error {
	switch endpointID {
	case OllamaEndpointID:
		if enabled && !platform.SupportsOllama() {
			return fmt.Errorf("Ollama setup is supported on macOS and Linux")
		}
		d.Config.Providers.Ollama.Enabled = enabled
	case MLXEndpointID:
		if enabled && !platform.SupportsMLX() {
			return fmt.Errorf("MLX requires an Apple silicon Mac")
		}
		d.Config.Providers.MLX.Enabled = enabled
	default:
		return fmt.Errorf("unknown provider %q", endpointID)
	}
	if enabled && d.discoveredProviderReady(endpointID) {
		d.SetProviderReady(endpointID, true)
	}
	return nil
}

func (d Draft) discoveredProviderReady(endpointID string) bool {
	for _, result := range d.Results {
		if result.Err != nil {
			continue
		}
		if result.Endpoint.ID == endpointID || (endpointID == OllamaEndpointID && result.Endpoint.Kind == topology.KindOllama) {
			return true
		}
	}
	return false
}
