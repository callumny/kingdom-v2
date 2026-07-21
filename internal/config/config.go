package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/callumny/kingdom/internal/topology"
)

// Config is the persisted application configuration.
type Config struct {
	Version           int               `json:"version"`
	Providers         Providers         `json:"providers"`
	CouncilEnabled    bool              `json:"council_enabled"`
	CouncilSize       int               `json:"council_size"`
	WorkerConcurrency int               `json:"worker_concurrency"`
	Topology          topology.Topology `json:"topology"`
}

// CurrentVersion is the supported on-disk schema version.
const CurrentVersion = 2

type OllamaPortMode string

const (
	OllamaDedicatedPorts OllamaPortMode = "dedicated"
	OllamaSharedPort     OllamaPortMode = "shared"
)

type Provider struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

type OllamaProvider struct {
	Enabled  bool           `json:"enabled"`
	Port     int            `json:"port"`
	PortMode OllamaPortMode `json:"port_mode"`
}

type Providers struct {
	Ollama OllamaProvider `json:"ollama"`
	MLX    Provider       `json:"mlx"`
}

func (p Providers) AnyEnabled() bool { return p.Ollama.Enabled || p.MLX.Enabled }

// Default returns setup-safe default configuration.
func Default() Config {
	return Config{
		Version:           CurrentVersion,
		CouncilEnabled:    true,
		CouncilSize:       3,
		WorkerConcurrency: 4,
		Providers: Providers{
			Ollama: OllamaProvider{Port: 11434, PortMode: OllamaDedicatedPorts},
			MLX:    Provider{Port: 8080},
		},
		Topology: topology.Default(),
	}
}

// Validate checks schema, bounds, and topology structure.
func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if c.Providers.Ollama.Port < 1 || c.Providers.Ollama.Port > 65535 {
		return fmt.Errorf("Ollama port must be 1..65535")
	}
	if c.Providers.MLX.Port < 1 || c.Providers.MLX.Port > 65535 {
		return fmt.Errorf("MLX port must be 1..65535")
	}
	if c.Providers.Ollama.PortMode != OllamaDedicatedPorts && c.Providers.Ollama.PortMode != OllamaSharedPort {
		return fmt.Errorf("Ollama port mode must be %q or %q", OllamaDedicatedPorts, OllamaSharedPort)
	}
	if c.CouncilSize < 1 || c.CouncilSize > 9 {
		return fmt.Errorf("council size must be 1..9")
	}
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 32 {
		return fmt.Errorf("worker concurrency must be 1..32")
	}
	if err := c.Topology.Validate(); err != nil {
		return err
	}
	return validateOllamaPortCapacity(c)
}

// IsReady reports whether configuration is valid and has required assignments.
func (c Config) IsReady() bool {
	if c.Validate() != nil || !c.Providers.AnyEnabled() || !c.Topology.IsReady() {
		return false
	}
	return !c.CouncilEnabled || c.Topology.Roles.Council.Complete()
}

// RequiresSetup reports whether the application should present initial setup UI.
func (c Config) RequiresSetup() bool { return !c.IsReady() }

// Load reads strict JSON configuration, returning defaults when the file is absent.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var c Config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("invalid config JSON: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Config{}, fmt.Errorf("invalid config JSON: trailing data")
	}
	if err := migrate(&c); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return c, nil
}

func migrate(c *Config) error {
	if c.Version == CurrentVersion {
		return nil
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}

	defaults := Default()
	c.Version = CurrentVersion
	c.Providers = defaults.Providers
	c.CouncilEnabled = true
	for _, endpoint := range c.Topology.Endpoints {
		switch endpoint.ID {
		case "mlx-local":
			c.Providers.MLX.Enabled = true
		case "ollama-local":
			c.Providers.Ollama.Enabled = true
		default:
			if endpoint.Kind == topology.KindOllama {
				c.Providers.Ollama.Enabled = true
			}
		}
	}
	if c.Topology.Roles.Council.Empty() && c.Topology.Roles.King.Complete() {
		c.Topology.Roles.Council = c.Topology.Roles.King
	}
	return nil
}

// Save validates and atomically replaces path using a same-directory temporary file.
// POSIX filesystems provide atomic replacement for same-directory rename; platforms
// that reject replacing an existing destination may return an error and preserve it.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := ensureDirectory(dir); err != nil {
		return fmt.Errorf("prepare config directory: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmp := f.Name()
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		_ = os.Remove(tmp)
	}()
	if err := f.Chmod(0600); err != nil {
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err = f.Write(b); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	closed = true
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("path exists and is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	var missing []string
	for p := path; ; p = filepath.Dir(p) {
		info, statErr := os.Stat(p)
		if statErr == nil {
			if !info.IsDir() {
				return fmt.Errorf("ancestor %q is not a directory", p)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		parent := filepath.Dir(p)
		missing = append(missing, p)
		if parent == p {
			break
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	for _, p := range missing {
		if err := os.Chmod(p, 0700); err != nil {
			return err
		}
	}
	return nil
}

// DefaultPath returns the per-user configuration path.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kingdom", "v2", "config.json"), nil
}
