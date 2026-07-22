package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestOnboardingStartsAtWelcomeThenProviders(t *testing.T) {
	w := Start(config.Default(), discovery.DefaultEndpoints())
	if w.State != StateProviders {
		t.Fatalf("initial state=%v, want providers", w.State)
	}
	if err := w.Continue(); err == nil || w.State != StateProviders {
		t.Fatalf("providers advanced without an enabled provider: state=%v err=%v", w.State, err)
	}
	w.Draft.Config.Providers.Ollama.Enabled = true
	w.Draft.ApplyResults([]EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0], Models: []discovery.Model{{ID: "model"}}}})
	if err := w.Continue(); err != nil || w.State != StateModels {
		t.Fatalf("providers continue: state=%v err=%v", w.State, err)
	}
	if err := w.Draft.ToggleModel(ModelRef{EndpointID: discovery.DefaultEndpoints()[0].ID, ModelID: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Continue(); err != nil || w.State != StateWizard {
		t.Fatalf("models continue: state=%v err=%v", w.State, err)
	}
	w.Back()
	if w.State != StateModels {
		t.Fatalf("Wizard back=%v, want models", w.State)
	}
	w.Back()
	if w.State != StateProviders {
		t.Fatalf("models back=%v, want providers", w.State)
	}
	w.Back()
	if w.State != StateProviders {
		t.Fatalf("providers back=%v, want providers", w.State)
	}
}

func TestEnabledProvidersMustBeReadyBeforeModels(t *testing.T) {
	w := Start(config.Default(), discovery.DefaultEndpoints())
	w.Draft.Config.Providers.MLX.Enabled = true
	w.Draft.ApplyResults([]EndpointResult{{Endpoint: discovery.DefaultEndpoints()[1], Err: context.DeadlineExceeded}})
	if err := w.Continue(); err == nil || !strings.Contains(err.Error(), "MLX") {
		t.Fatalf("unready provider advanced: state=%v err=%v", w.State, err)
	}
	w.Draft.SetProviderReady(MLXEndpointID, true)
	if err := w.Continue(); err != nil || w.State != StateModels {
		t.Fatalf("ready provider did not advance: state=%v err=%v", w.State, err)
	}
}

func TestMergeCandidatesOverride(t *testing.T) {
	a := topology.Endpoint{ID: "x", Name: "default", Kind: topology.KindOllama, BaseURL: "http://localhost:1"}
	b := a
	b.Name = "configured"
	r := MergeCandidates([]topology.Endpoint{a}, []topology.Endpoint{b})
	if len(r) != 1 || r[0].Name != "configured" {
		t.Fatalf("%+v", r)
	}
}
func TestCustomIDAndPersistence(t *testing.T) {
	e, err := ValidateCustom(topology.KindOllama, "x", "http://localhost:1234/")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDraft(config.Default(), nil)
	d.Config.Topology.Endpoints = []topology.Endpoint{e}
	d.AssignKing(topology.Assignment{EndpointID: e.ID, Model: "m"})
	d.AssignWorker(topology.Assignment{EndpointID: e.ID, Model: "m"})
	if len(d.PersistenceEndpoints(nil)) != 1 {
		t.Fatal("custom endpoint not persisted")
	}
}

func TestStableCustomIDDistinctNormalizedURLs(t *testing.T) {
	// These URLs collided with the former punctuation-replacement scheme.
	left := "http://localhost/a:b"
	right := "http://localhost/a-b"
	leftID := StableCustomID(topology.KindOllama, left)
	rightID := StableCustomID(topology.KindOllama, right)
	if leftID == rightID {
		t.Fatalf("collision: %q and %q both produced %q", left, right, leftID)
	}

	// Scheme/host case and trailing slash/query/fragment normalization are stable.
	normalized := StableCustomID(topology.KindOllama, "HTTP://LOCALHOST:1234/path/")
	equivalent := StableCustomID(topology.KindOllama, "http://localhost:1234/path/?ignored=yes#fragment")
	if normalized != equivalent {
		t.Fatalf("normalized URLs differ: %q != %q", normalized, equivalent)
	}

	otherKind := StableCustomID(topology.KindOpenAICompatible, "http://localhost:1234/path/")
	if normalized == otherKind {
		t.Fatal("endpoint kind must contribute to custom ID")
	}
	if strings.Contains(normalized, "localhost") || strings.Contains(normalized, "/path") {
		t.Fatalf("custom ID leaks URL data: %q", normalized)
	}
}

func TestPerformanceBounds(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-1, 1}, {1, 1}, {9, 9}, {10, 9}} {
		if got := ClampCouncilSize(tc.in); got != tc.want {
			t.Fatalf("council %d => %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want int }{{-1, 1}, {1, 1}, {32, 32}, {33, 32}} {
		if got := ClampWorkerConcurrency(tc.in); got != tc.want {
			t.Fatalf("worker %d => %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestProviderPortsDetermineLoopbackEndpoints(t *testing.T) {
	c := config.Default()
	c.Providers.Ollama.Port = 12001
	c.Providers.MLX.Port = 12002
	endpoints := ApplyProviderPorts(discovery.DefaultEndpoints(), c.Providers)
	if endpoints[0].BaseURL != "http://127.0.0.1:12001" || endpoints[1].BaseURL != "http://127.0.0.1:12002/v1" {
		t.Fatalf("endpoints=%+v", endpoints)
	}
	if discovery.DefaultEndpoints()[0].BaseURL != "http://localhost:11434" {
		t.Fatal("input endpoints were mutated")
	}
}
