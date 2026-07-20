package localmodels

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOllamaUsesOfficialScriptAfterPlatformValidation(t *testing.T) {
	system := &fakeSystem{paths: map[string]string{
		"curl": "/usr/bin/curl",
		"sh":   "/bin/sh",
	}}
	installer := NewInstaller(system, t.TempDir())
	if err := installer.Install(context.Background(), KindOllama, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if len(system.run) != 2 {
		t.Fatalf("commands=%+v", system.run)
	}
	download, execute := system.run[0], system.run[1]
	if download.name != "/usr/bin/curl" || len(download.args) != 4 || download.args[0] != "-fsSL" || download.args[1] != OllamaInstallURL || download.args[2] != "-o" {
		t.Fatalf("download=%+v", download)
	}
	if execute.name != "/bin/sh" || len(execute.args) != 1 || execute.args[0] != download.args[3] {
		t.Fatalf("execute=%+v download=%+v", execute, download)
	}
	if !strings.HasPrefix(filepath.Base(download.args[3]), "ollama-install-") {
		t.Fatalf("temporary script=%q", download.args[3])
	}
}

func TestInstallMLXCreatesKingdomManagedEnvironment(t *testing.T) {
	root := t.TempDir()
	system := &fakeSystem{
		paths:   map[string]string{"python3.13": "/opt/homebrew/bin/python3.13", "python3": "/usr/bin/python3"},
		outputs: map[string][]byte{"/opt/homebrew/bin/python3.13 --version": []byte("Python 3.13.7\n")},
	}
	installer := NewInstaller(system, root)
	var progress []InstallProgress
	if err := installer.InstallWithProgress(context.Background(), KindMLX, "darwin", "arm64", func(update InstallProgress) { progress = append(progress, update) }); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(root, "mlx")
	want := []commandCall{
		{name: "/opt/homebrew/bin/python3.13", args: []string{"--version"}},
		{name: "/opt/homebrew/bin/python3.13", args: []string{"-m", "venv", "--clear", environment}},
		{name: filepath.Join(environment, "bin", "python"), args: []string{"-m", "pip", "install", "--upgrade", "pip", "setuptools", "wheel"}},
		{name: filepath.Join(environment, "bin", "python"), args: []string{"-m", "pip", "install", "--upgrade", "mlx-lm"}},
	}
	if len(system.run) != len(want) {
		t.Fatalf("commands=%+v", system.run)
	}
	for index := range want {
		if system.run[index].name != want[index].name || strings.Join(system.run[index].args, "\x00") != strings.Join(want[index].args, "\x00") {
			t.Fatalf("command %d=%+v want %+v", index, system.run[index], want[index])
		}
	}
	if len(progress) < 2 || progress[len(progress)-1].Completed != progress[len(progress)-1].Total || progress[len(progress)-1].Message != "MLX is installed" {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestInstallMLXRejectsOldPythonWithActionableError(t *testing.T) {
	system := &fakeSystem{
		paths:   map[string]string{"python3": "/usr/bin/python3"},
		outputs: map[string][]byte{"/usr/bin/python3 --version": []byte("Python 3.9.6\n")},
	}
	err := NewInstaller(system, t.TempDir()).Install(context.Background(), KindMLX, "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "Python 3.10") {
		t.Fatalf("error=%v", err)
	}
}

func TestInstallMLXSkipsStalePythonSymlink(t *testing.T) {
	root := t.TempDir()
	system := &fakeSystem{
		paths: map[string]string{
			"python3.13": "/opt/homebrew/bin/python3.13",
			"python3.12": "/opt/homebrew/bin/python3.12",
		},
		outputs: map[string][]byte{"/opt/homebrew/bin/python3.12 --version": []byte("Python 3.12.13\n")},
		errors:  map[string]error{"/opt/homebrew/bin/python3.13 --version": errors.New("stale symlink")},
	}
	if err := NewInstaller(system, root).Install(context.Background(), KindMLX, "darwin", "arm64"); err != nil {
		t.Fatal(err)
	}
	if len(system.run) < 3 || system.run[2].name != "/opt/homebrew/bin/python3.12" || strings.Join(system.run[2].args, " ") != "-m venv --clear "+filepath.Join(root, "mlx") {
		t.Fatalf("commands=%+v", system.run)
	}
}

func TestInstallRejectsUnsupportedPlatformsAndProvider(t *testing.T) {
	installer := NewInstaller(&fakeSystem{}, t.TempDir())
	for _, test := range []struct {
		kind     Kind
		os, arch string
	}{
		{KindOllama, "windows", "amd64"},
		{KindMLX, "linux", "arm64"},
		{KindMLX, "darwin", "amd64"},
		{Kind("unknown"), "darwin", "arm64"},
	} {
		if err := installer.Install(context.Background(), test.kind, test.os, test.arch); err == nil {
			t.Fatalf("Install(%q, %q, %q) succeeded", test.kind, test.os, test.arch)
		}
	}
}
