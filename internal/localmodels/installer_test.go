package localmodels

import (
	"context"
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
	system := &fakeSystem{paths: map[string]string{"python3": "/usr/bin/python3"}}
	installer := NewInstaller(system, root)
	if err := installer.Install(context.Background(), KindMLX, "darwin", "arm64"); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(root, "mlx")
	want := []commandCall{
		{name: "/usr/bin/python3", args: []string{"-m", "venv", environment}},
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
