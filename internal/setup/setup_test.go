package setup

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestOnboardingStartsAtWelcomeThenProviders(t *testing.T) {
	w := Start(config.Default(), discovery.DefaultEndpoints())
	if w.State != StateWelcome {
		t.Fatalf("initial state=%v, want welcome", w.State)
	}
	if err := w.Continue(); err != nil || w.State != StateProviders {
		t.Fatalf("welcome continue: state=%v err=%v", w.State, err)
	}
	if err := w.Continue(); err == nil || w.State != StateProviders {
		t.Fatalf("providers advanced without models: state=%v err=%v", w.State, err)
	}
	w.Draft.ApplyResults([]EndpointResult{{Endpoint: discovery.DefaultEndpoints()[0], Models: []discovery.Model{{ID: "model"}}}})
	if err := w.Continue(); err != nil || w.State != StateModels {
		t.Fatalf("providers continue: state=%v err=%v", w.State, err)
	}
	if err := w.Draft.ToggleModel(ModelRef{EndpointID: discovery.DefaultEndpoints()[0].ID, ModelID: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Continue(); err != nil || w.State != StateRoles {
		t.Fatalf("models continue: state=%v err=%v", w.State, err)
	}
	w.Back()
	if w.State != StateModels {
		t.Fatalf("roles back=%v, want models", w.State)
	}
	w.Back()
	if w.State != StateProviders {
		t.Fatalf("models back=%v, want providers", w.State)
	}
	w.Back()
	if w.State != StateWelcome {
		t.Fatalf("providers back=%v, want welcome", w.State)
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
