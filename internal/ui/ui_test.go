package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func TestCustomFormQIsText(t *testing.T) {
	f := NewCustomEndpointForm()
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "q"}))
	if f.Name.Value() != "q" {
		t.Fatalf("q was not delivered to focused input: %q", f.Name.Value())
	}
}

func TestQTypesInActiveCustomForm(t *testing.T) {
	f := NewCustomEndpointForm()
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "q"}))
	if f.Name.Value() != "q" {
		t.Fatalf("q not inserted: %q", f.Name.Value())
	}
}

func TestCustomFormCanSelectProviderKind(t *testing.T) {
	f := NewCustomEndpointForm()
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "o"}))
	if f.Kind != topology.KindOpenAICompatible {
		t.Fatalf("kind=%q", f.Kind)
	}
	f.Name.SetValue("x")
	f.BaseURL.SetValue("http://localhost")
	if _, err := f.Endpoint(); err != nil {
		t.Fatal(err)
	}
}

func TestTinyTerminalViewDoesNotPanic(t *testing.T) {
	v := View(1, 1, true)
	if v.Content == "" || !strings.Contains(v.Content, "Kingdom") {
		t.Fatalf("unexpected tiny view: %q", v.Content)
	}
}

func TestCustomFormRendersAndShowsValidationError(t *testing.T) {
	f := NewCustomEndpointForm()
	if !strings.Contains(f.View(), "Provider") || !strings.Contains(f.View(), "Name") || !strings.Contains(f.View(), "URL") {
		t.Fatal("missing form fields")
	}
	f.Name.SetValue("")
	f.BaseURL.SetValue("")
	if _, err := f.Endpoint(); err == nil {
		t.Fatal("expected validation error")
	}
	f.Err = errInvalidForTest{}
	if !strings.Contains(f.View(), "invalid") {
		t.Fatal("missing validation error")
	}
}

type errInvalidForTest struct{}

func (errInvalidForTest) Error() string { return "invalid" }

func TestPerformanceViewShowsFocus(t *testing.T) {
	if !strings.Contains(View(80, 20, true, nil).Content, "Setup required") {
		t.Fatal("missing setup")
	}
}

func TestProviderFocusDoesNotTypeIntoURL(t *testing.T) {
	f := NewCustomEndpointForm()
	f.Focus = 0
	f, _ = f.Update(tea.KeyPressMsg(tea.Key{Text: "x"}))
	if f.BaseURL.Value() != "" {
		t.Fatal("provider key typed into URL")
	}
}

func TestPerformanceMarkerFollowsFocus(t *testing.T) {
	p := Presentation{PerfFocus: 1}
	if !strings.Contains(ViewWithPresentation(80, 20, true, nil, p).Content, "> Worker concurrency") {
		t.Fatal("worker marker missing")
	}
}

func TestPresentationRendererStates(t *testing.T) {
	w := &setup.Workflow{State: setup.StateDiscovery, Draft: setup.Draft{Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e1", Name: "ep"}, Models: []discovery.Model{{ID: "m1"}}}}}}
	scanning := ViewWithPresentation(80, 40, true, w, Presentation{Scanning: true}).Content
	if !strings.Contains(scanning, "Scanning...") {
		t.Fatal("scanning")
	}
	if strings.Contains(scanning, "Enter:") {
		t.Fatalf("scanning view advertised a blocked action: %s", scanning)
	}
	roles := *w
	roles.State = setup.StateRoles
	roles.Draft.CouncilUseKing = true
	s := ViewWithPresentation(80, 40, true, &roles, Presentation{ModelIndex: 0, Role: 1}).Content
	for _, want := range []string{"Worker", "> ep (e1) / m1", "Council: uses King"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s", want, s)
		}
	}
	perf := *w
	perf.State = setup.StatePerformance
	if !strings.Contains(ViewWithPresentation(80, 40, true, &perf, Presentation{PerfFocus: 1}).Content, "> Worker concurrency") {
		t.Fatal("focus")
	}
	rev := *w
	rev.State = setup.StateReview
	rev.Err = errInvalidForTest{}
	if !strings.Contains(ViewWithPresentation(80, 40, true, &rev, Presentation{Saving: true}).Content, "Saving...") {
		t.Fatal("saving")
	}
	if got := strings.Count(ViewWithPresentation(80, 3, true, &rev, Presentation{}).Content, "\n") + 1; got > 3 {
		t.Fatalf("height=%d", got)
	}
	f := NewCustomEndpointForm()
	f.Err = errInvalidForTest{}
	if !strings.Contains(ViewWithPresentation(80, 40, true, &rev, Presentation{FormActive: true, Form: &f}).Content, "Error: invalid") {
		t.Fatal("form error")
	}
}

func TestDiscoveryViewGuidesModelSetup(t *testing.T) {
	w := &setup.Workflow{State: setup.StateDiscovery, Draft: setup.Draft{Results: []setup.EndpointResult{{
		Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
		Err:      errInvalidForTest{},
	}}}}
	view := ViewWithPresentation(100, 40, true, w, Presentation{}).Content
	for _, want := range []string{"Enter: set up a model", "m: models", "No models discovered"} {
		if !strings.Contains(view, want) {
			t.Fatalf("discovery guidance missing %q: %s", want, view)
		}
	}
}

func TestReviewRendersExactDraftSummary(t *testing.T) {
	w := &setup.Workflow{State: setup.StateReview, Draft: setup.Draft{Config: config.Default(), Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "unused", Name: "Unused", BaseURL: "u"}}}}}
	w.Draft.Config.Topology.Roles.King = topology.Assignment{EndpointID: "king", Model: "km"}
	w.Draft.Config.Topology.Roles.Worker = topology.Assignment{EndpointID: "worker", Model: "wm"}
	w.Draft.CouncilUseKing = true
	s := ViewWithPresentation(80, 40, true, w, Presentation{PreviousEndpoints: []topology.Endpoint{{ID: "offline", Name: "Offline", BaseURL: "o"}}}).Content
	for _, want := range []string{"King: king/km", "Worker: worker/wm", "Council: uses King", "Council size", "Worker concurrency", "Offline"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "Unused") {
		t.Fatal("unused endpoint rendered")
	}
}
