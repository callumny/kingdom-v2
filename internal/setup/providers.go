package setup

import "fmt"

const (
	OllamaEndpointID = "ollama-local"
	MLXEndpointID    = "mlx-local"
)

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
	return nil
}
