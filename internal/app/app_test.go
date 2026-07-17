package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/topology"
)

func TestNewModelRendersFoundation(t *testing.T) {
	view := New(config.Default()).View()
	if view.Content == "" || !strings.Contains(view.Content, "Kingdom") {
		t.Fatalf("expected foundation view, got %q", view.Content)
	}
}

func TestViewReflectsSetupState(t *testing.T) {
	if got := New(config.Default()).View().Content; !strings.Contains(got, "Setup required") {
		t.Fatalf("incomplete config view = %q, want setup status", got)
	}

	c := completeConfig()
	if got := New(c).View().Content; !strings.Contains(got, "Configuration ready") {
		t.Fatalf("complete config view = %q, want ready status", got)
	}
}

func TestConfigSetupStateIntegration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg)
	if !m.RequiresSetup() {
		t.Fatal("missing config should require setup")
	}
	c := completeConfig()
	if err := config.Save(p, c); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	m = New(cfg)
	if m.RequiresSetup() {
		t.Fatal("saved complete config should not require setup")
	}
}

func completeConfig() config.Config {
	c := config.Default()
	c.Topology.Endpoints = []topology.Endpoint{{ID: "local", Name: "Local", Kind: topology.KindOllama, BaseURL: "http://localhost"}}
	c.Topology.Roles.King = topology.Assignment{EndpointID: "local", Model: "k"}
	c.Topology.Roles.Worker = topology.Assignment{EndpointID: "local", Model: "w"}
	return c
}

func TestUpdateQuitKeys(t *testing.T) {
	keys := []tea.Key{{Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}}
	for _, key := range keys {
		model, cmd := New(config.Default()).Update(tea.KeyPressMsg(key))
		if model == nil || cmd == nil {
			t.Fatalf("key %q did not request quit", key.String())
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Fatalf("key %q returned %T, want tea.QuitMsg", key.String(), msg)
		}
	}
}
