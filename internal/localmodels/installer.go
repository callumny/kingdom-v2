package localmodels

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const OllamaInstallURL = "https://ollama.com/install.sh"

// Installer performs only provider installation. The TUI owns confirmation;
// keeping it outside this type makes accidental auto-installation impossible.
type Installer struct {
	system System
	root   string
}

func NewInstaller(system System, root string) *Installer {
	return &Installer{system: system, root: filepath.Clean(root)}
}

func (i *Installer) Install(ctx context.Context, kind Kind, operatingSystem, architecture string) error {
	if i == nil || i.system == nil {
		return errors.New("provider installer is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch kind {
	case KindOllama:
		if operatingSystem != "darwin" && operatingSystem != "linux" {
			return errors.New("Ollama setup is supported on macOS and Linux")
		}
		return i.installOllama(ctx)
	case KindMLX:
		if operatingSystem != "darwin" || architecture != "arm64" {
			return errors.New("MLX requires an Apple silicon Mac")
		}
		return i.installMLX(ctx)
	default:
		return fmt.Errorf("unknown local runtime %q", kind)
	}
}

func (i *Installer) installOllama(ctx context.Context) error {
	if executable, err := i.system.LookPath("ollama"); err == nil && executable != "" {
		return nil
	}
	curl, err := i.system.LookPath("curl")
	if err != nil {
		return errors.New("install Ollama: curl is required")
	}
	shell, err := i.system.LookPath("sh")
	if err != nil {
		return errors.New("install Ollama: sh is required")
	}
	downloads := filepath.Join(i.root, "downloads")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		return fmt.Errorf("prepare Ollama installer: %w", err)
	}
	file, err := os.CreateTemp(downloads, "ollama-install-*.sh")
	if err != nil {
		return fmt.Errorf("prepare Ollama installer: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return fmt.Errorf("prepare Ollama installer: %w", err)
	}
	defer os.Remove(path)
	if _, err := i.system.Output(ctx, curl, "-fsSL", OllamaInstallURL, "-o", path); err != nil {
		return fmt.Errorf("download Ollama installer: %w", err)
	}
	if _, err := i.system.Output(ctx, shell, path); err != nil {
		return fmt.Errorf("run Ollama installer: %w", err)
	}
	return nil
}

func (i *Installer) installMLX(ctx context.Context) error {
	python, err := i.system.LookPath("python3")
	if err != nil {
		return errors.New("install MLX: Python 3 is required")
	}
	environment := filepath.Join(i.root, "mlx")
	if err := os.MkdirAll(i.root, 0700); err != nil {
		return fmt.Errorf("prepare MLX environment: %w", err)
	}
	if _, err := i.system.Output(ctx, python, "-m", "venv", environment); err != nil {
		return fmt.Errorf("create MLX environment: %w", err)
	}
	managedPython := filepath.Join(environment, "bin", "python")
	if _, err := i.system.Output(ctx, managedPython, "-m", "pip", "install", "--upgrade", "mlx-lm"); err != nil {
		return fmt.Errorf("install MLX package: %w", err)
	}
	return nil
}
