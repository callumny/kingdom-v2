package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/charmbracelet/x/ansi"
)

func TestProvidersExplainLocalModelsAndTradeoffs(t *testing.T) {
	w := &setup.Workflow{State: setup.StateProviders, Draft: setup.NewDraft(config.Default(), discovery.DefaultEndpoints())}
	view := ViewWithPresentation(100, 32, true, w, Presentation{Scanning: true}).Content
	for _, want := range []string{
		"entirely on your machine",
		"Ollama",
		"MLX",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome missing %q: %s", want, view)
		}
	}
	assertViewFits(t, view, 100, 32)
}

func TestProviderViewHasProgressStatusAndContextualHelp(t *testing.T) {
	endpoints := discovery.DefaultEndpoints()
	w := &setup.Workflow{State: setup.StateProviders, Draft: setup.NewDraft(config.Default(), endpoints)}
	w.Draft.ApplyResults([]setup.EndpointResult{
		{Endpoint: endpoints[0], Models: []discovery.Model{{ID: "one"}, {ID: "two"}}},
		{Endpoint: endpoints[1], Err: errInvalidForTest{}},
	})
	view := ViewWithPresentation(100, 32, true, w, Presentation{ProviderCursor: 1}).Content
	for _, want := range []string{
		"1 Providers",
		"2 Models",
		"Set up model providers",
		"Ollama",
		"2 models",
		"MLX",
		"Unavailable",
		"Space Toggle",
		"Enter Continue",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("providers missing %q: %s", want, view)
		}
	}
	assertViewFits(t, view, 100, 32)
}

func TestModelRowsUseAlignedProviderStatusAndNameColumns(t *testing.T) {
	cfg := config.Default()
	w := &setup.Workflow{State: setup.StateModels, Draft: setup.NewDraft(cfg, nil)}
	w.Draft.ReplaceCatalog([]setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "qwen3:14b"}, Endpoint: topology.Endpoint{Name: "Ollama"}, Installed: true},
		{Ref: setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: "mlx-community/Qwen3-8B-4bit"}, Endpoint: topology.Endpoint{Name: "MLX"}, Installed: false},
	})
	view := ansi.Strip(ViewWithPresentation(100, 40, true, w, Presentation{}).Content)
	if !strings.Contains(view, "Provider") || !strings.Contains(view, "Status") || !strings.Contains(view, "Model") {
		t.Fatalf("missing model table header: %s", view)
	}
	lines := strings.Split(view, "\n")
	var first, second string
	for _, line := range lines {
		if strings.Contains(line, "qwen3:14b") {
			first = line
		}
		if strings.Contains(line, "mlx-community/Qwen3-8B-4bit") {
			second = line
		}
	}
	if first == "" || second == "" {
		t.Fatalf("model rows missing: %s", view)
	}
	for _, column := range []struct {
		first  string
		second string
	}{
		{"Ollama", "MLX"},
		{"Installed", "Download"},
		{"qwen3:14b", "mlx-community/Qwen3-8B-4bit"},
	} {
		firstIndex := strings.Index(first, column.first)
		secondIndex := strings.Index(second, column.second)
		if ansi.StringWidth(first[:firstIndex]) != ansi.StringWidth(second[:secondIndex]) {
			t.Fatalf("column %q/%q is not aligned:\n%s\n%s", column.first, column.second, first, second)
		}
	}
}

func TestWizardViewMatchesConciseJourney(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaDedicatedPorts)
	w.State = setup.StateWizard
	wizardView := ViewWithPresentation(100, 40, true, w, Presentation{
		WizardModel:     "small · fast setup model",
		WizardMessages:  []string{"Wizard: I prepared a sensible setup."},
		WizardReady:     true,
		WizardPreparing: true,
	}).Content
	for _, want := range []string{"Wizard", "small · fast setup model", "Starting the local Wizard model", "I prepared a sensible setup", "King", "Worker", "Council", "Concurrent workers", "Apply & launch", "Ctrl+Enter Send", "Tab Manual setup"} {
		if !strings.Contains(wizardView, want) {
			t.Fatalf("Wizard missing %q: %s", want, wizardView)
		}
	}
}

func TestWizardViewWrapsLongConversationMessages(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaDedicatedPorts)
	w.State = setup.StateWizard
	message := "Wizard: I prepared sensible defaults and selected your Worker model for a fast setup conversation. You can apply them now or ask for one change."
	view := ansi.Strip(ViewWithPresentation(64, 40, true, w, Presentation{
		WizardMessages: []string{message},
		WizardReady:    true,
	}).Content)

	for _, want := range []string{"fast setup", "conversation.", "ask for one change."} {
		if !strings.Contains(view, want) {
			t.Fatalf("wrapped Wizard message missing %q:\n%s", want, view)
		}
	}
	assertViewFits(t, view, 64, 40)
}

func assertViewFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("height=%d, want <= %d", len(lines), height)
	}
	for index, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line %d width=%d, want <= %d: %q", index, got, width, line)
		}
	}
}

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
	if v.Content == "" || ansi.StringWidth(v.Content) > 1 {
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
	if !strings.Contains(ViewWithPresentation(80, 20, true, nil, p).Content, "> Concurrent workers") {
		t.Fatal("worker marker missing")
	}
}

func TestPerformanceViewExplainsDedicatedOllamaServers(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaDedicatedPorts)
	view := ViewWithPresentation(100, 40, true, w, Presentation{PerfFocus: 2}).Content
	for _, want := range []string{
		"Advanced performance",
		"Council members",
		"Concurrent workers",
		"> Separate Ollama servers",
		"ON",
		"Recommended",
		"large → 127.0.0.1:11434",
		"small → 127.0.0.1:11435",
		"MLX is unaffected",
		"Hardware note",
		"more RAM",
		"Space Toggle",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("performance view missing %q: %s", want, view)
		}
	}
	assertViewFits(t, view, 100, 40)
}

func TestPerformanceViewExplainsSharedOllamaServer(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaSharedPort)
	view := ViewWithPresentation(100, 40, true, w, Presentation{PerfFocus: 2}).Content
	for _, want := range []string{
		"Separate Ollama servers",
		"OFF",
		"large + small → 127.0.0.1:11434",
		"less memory",
		"contention",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("shared performance view missing %q: %s", want, view)
		}
	}
}

func TestPerformanceViewHidesOllamaControlForMLXOnlySetup(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaDedicatedPorts)
	w.Draft.Config.Providers.Ollama.Enabled = false
	w.Draft.Config.Providers.MLX.Enabled = true
	w.Draft.Config.Topology.Endpoints = []topology.Endpoint{{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"}}
	w.Draft.Config.Topology.Roles.King = topology.Assignment{EndpointID: setup.MLXEndpointID, Model: "large"}
	w.Draft.Config.Topology.Roles.Worker = topology.Assignment{EndpointID: setup.MLXEndpointID, Model: "small"}
	w.Draft.Config.Topology.Roles.Council = w.Draft.Config.Topology.Roles.King

	view := ViewWithPresentation(100, 40, true, w, Presentation{PerfFocus: 1}).Content
	if strings.Contains(view, "Separate Ollama servers") || strings.Contains(view, "Space Toggle") {
		t.Fatalf("MLX-only view exposed Ollama controls: %s", view)
	}
}

func TestReviewShowsOllamaServerPlan(t *testing.T) {
	w := managedOllamaPerformanceWorkflow(config.OllamaDedicatedPorts)
	w.State = setup.StateReview
	view := ViewWithPresentation(100, 40, true, w, Presentation{}).Content
	for _, want := range []string{"Ollama servers: separate", "large → 127.0.0.1:11434", "small → 127.0.0.1:11435"} {
		if !strings.Contains(view, want) {
			t.Fatalf("review missing %q: %s", want, view)
		}
	}
}

func managedOllamaPerformanceWorkflow(mode config.OllamaPortMode) *setup.Workflow {
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.Ollama.PortMode = mode
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "large"}
	cfg.Topology.Roles.Worker = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "small"}
	cfg.Topology.Roles.Council = cfg.Topology.Roles.King
	return &setup.Workflow{State: setup.StatePerformance, Draft: setup.NewDraft(cfg, nil)}
}

func TestPresentationRendererStates(t *testing.T) {
	w := &setup.Workflow{State: setup.StateProviders, Draft: setup.Draft{Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e1", Name: "ep"}, Models: []discovery.Model{{ID: "m1"}}}}}}
	scanning := ViewWithPresentation(80, 40, true, w, Presentation{Scanning: true}).Content
	if !strings.Contains(scanning, "Checking local providers") {
		t.Fatal("scanning")
	}
	if strings.Contains(scanning, "Enter Continue") {
		t.Fatalf("scanning view advertised a blocked action: %s", scanning)
	}
	roles := *w
	roles.State = setup.StateRoles
	roles.Draft.Config.CouncilEnabled = false
	if err := roles.Draft.ToggleModel(setup.ModelRef{EndpointID: "e1", ModelID: "m1"}); err != nil {
		t.Fatal(err)
	}
	s := ViewWithPresentation(80, 40, true, &roles, Presentation{ModelIndex: 0, Role: 1}).Content
	for _, want := range []string{"Editing: Worker", "ep / m1", "disabled"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s", want, s)
		}
	}
	perf := *w
	perf.State = setup.StatePerformance
	if !strings.Contains(ViewWithPresentation(80, 40, true, &perf, Presentation{PerfFocus: 1}).Content, "> Concurrent workers") {
		t.Fatal("focus")
	}
	rev := *w
	rev.State = setup.StateReview
	rev.Err = errInvalidForTest{}
	if !strings.Contains(ViewWithPresentation(80, 40, true, &rev, Presentation{Saving: true}).Content, "Saving…") {
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

func TestProviderViewGuidesUnavailableModelSetup(t *testing.T) {
	w := &setup.Workflow{State: setup.StateProviders, Draft: setup.Draft{Results: []setup.EndpointResult{{
		Endpoint: topology.Endpoint{ID: "ollama-local", Name: "Ollama"},
		Err:      errInvalidForTest{},
	}}}}
	view := ViewWithPresentation(100, 40, true, w, Presentation{}).Content
	for _, want := range []string{"Set up model providers", "Space Toggle", "Unavailable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("discovery guidance missing %q: %s", want, view)
		}
	}
}

func TestReviewRendersExactDraftSummary(t *testing.T) {
	w := &setup.Workflow{State: setup.StateReview, Draft: setup.Draft{Config: config.Default(), Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "unused", Name: "Unused", BaseURL: "u"}}}}}
	w.Draft.Config.Topology.Roles.King = topology.Assignment{EndpointID: "king", Model: "km"}
	w.Draft.Config.Topology.Roles.Worker = topology.Assignment{EndpointID: "worker", Model: "wm"}
	w.Draft.Config.CouncilEnabled = false
	s := ViewWithPresentation(80, 40, true, w, Presentation{PreviousEndpoints: []topology.Endpoint{{ID: "offline", Name: "Offline", BaseURL: "o"}}}).Content
	for _, want := range []string{"King: king/km", "Worker: worker/wm", "Council: disabled", "Council size", "Concurrent workers", "Offline"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "Unused") {
		t.Fatal("unused endpoint rendered")
	}
}
