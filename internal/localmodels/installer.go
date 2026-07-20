package localmodels

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const OllamaInstallURL = "https://ollama.com/install.sh"

// Installer performs only provider installation. The TUI owns confirmation;
// keeping it outside this type makes accidental auto-installation impossible.
type Installer struct {
	system System
	root   string
}

type InstallProgress struct {
	Completed int
	Total     int
	Message   string
}

type ProgressReporter func(InstallProgress)

func NewInstaller(system System, root string) *Installer {
	return &Installer{system: system, root: filepath.Clean(root)}
}

func (i *Installer) Install(ctx context.Context, kind Kind, operatingSystem, architecture string) error {
	return i.InstallWithProgress(ctx, kind, operatingSystem, architecture, nil)
}

func (i *Installer) InstallWithProgress(ctx context.Context, kind Kind, operatingSystem, architecture string, report ProgressReporter) error {
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
		return i.installOllama(ctx, report)
	case KindMLX:
		if operatingSystem != "darwin" || architecture != "arm64" {
			return errors.New("MLX requires an Apple silicon Mac")
		}
		return i.installMLX(ctx, report)
	default:
		return fmt.Errorf("unknown local runtime %q", kind)
	}
}

func (i *Installer) installOllama(ctx context.Context, report ProgressReporter) error {
	reportProgress(report, 0, 2, "Checking Ollama")
	if executable, err := i.system.LookPath("ollama"); err == nil && executable != "" {
		reportProgress(report, 2, 2, "Ollama is installed")
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
	if output, err := i.system.Output(ctx, curl, "-fsSL", OllamaInstallURL, "-o", path); err != nil {
		return commandFailure("download Ollama installer", output, err)
	}
	reportProgress(report, 1, 2, "Installing Ollama")
	if output, err := i.system.Output(ctx, shell, path); err != nil {
		return commandFailure("run Ollama installer", output, err)
	}
	reportProgress(report, 2, 2, "Ollama is installed")
	return nil
}

func (i *Installer) installMLX(ctx context.Context, report ProgressReporter) error {
	reportProgress(report, 0, 4, "Finding a compatible Python")
	python, err := i.compatiblePython(ctx)
	if err != nil {
		return err
	}
	environment := filepath.Join(i.root, "mlx")
	if err := os.MkdirAll(i.root, 0700); err != nil {
		return fmt.Errorf("prepare MLX environment: %w", err)
	}
	if output, err := i.system.Output(ctx, python, "-m", "venv", "--clear", environment); err != nil {
		return commandFailure("create MLX environment", output, err)
	}
	reportProgress(report, 1, 4, "Created Kingdom's MLX environment")
	managedPython := filepath.Join(environment, "bin", "python")
	if output, err := i.system.Output(ctx, managedPython, "-m", "pip", "install", "--upgrade", "pip", "setuptools", "wheel"); err != nil {
		return commandFailure("update Python packaging tools", output, err)
	}
	reportProgress(report, 2, 4, "Updated Python packaging tools")
	reportProgress(report, 3, 4, "Installing MLX packages")
	if output, err := i.system.Output(ctx, managedPython, "-m", "pip", "install", "--upgrade", "mlx-lm"); err != nil {
		return commandFailure("install MLX package", output, err)
	}
	reportProgress(report, 4, 4, "MLX is installed")
	return nil
}

var pythonVersionPattern = regexp.MustCompile(`Python\s+(\d+)\.(\d+)`)

func (i *Installer) compatiblePython(ctx context.Context) (string, error) {
	for _, name := range []string{"python3.13", "python3.12", "python3.11", "python3.10", "python3.14", "python3"} {
		path, err := i.system.LookPath(name)
		if err != nil || path == "" {
			continue
		}
		output, err := i.system.Output(ctx, path, "--version")
		if err != nil {
			continue
		}
		match := pythonVersionPattern.FindStringSubmatch(string(output))
		if len(match) != 3 {
			continue
		}
		major, _ := strconv.Atoi(match[1])
		minor, _ := strconv.Atoi(match[2])
		if major > 3 || (major == 3 && minor >= 10) {
			return path, nil
		}
	}
	return "", errors.New("install MLX: Python 3.10 or newer is required; install a current Python and retry")
}

func reportProgress(report ProgressReporter, completed, total int, message string) {
	if report != nil {
		report(InstallProgress{Completed: completed, Total: total, Message: message})
	}
}

func commandFailure(action string, output []byte, err error) error {
	detail := strings.Join(strings.Fields(strings.ToValidUTF8(string(output), "�")), " ")
	if len(detail) > 1200 {
		detail = detail[len(detail)-1200:]
	}
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}
