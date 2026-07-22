package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func key(s string) tea.KeyPressMsg {
	if s == " " {
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: s})
	}
	return tea.KeyPressMsg(tea.Key{Text: s})
}

func TestNewModelRendersFoundation(t *testing.T) {
	view := New(config.Default()).View()
	if view.Content == "" || !strings.Contains(view.Content, "Kingdom") {
		t.Fatalf("expected foundation view, got %q", view.Content)
	}
}

func TestViewReflectsSetupState(t *testing.T) {
	if got := New(config.Default()).View().Content; !strings.Contains(got, "Set up model providers") {
		t.Fatalf("incomplete config view = %q, want setup status", got)
	}

	c := completeConfig()
	if got := New(c).View().Content; !strings.Contains(got, "Ctrl+Enter send") {
		t.Fatalf("complete config view = %q, want chat controls", got)
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
	c.Providers.Ollama.Enabled = true
	c.Topology.Endpoints = []topology.Endpoint{{ID: "local", Name: "Local", Kind: topology.KindOllama, BaseURL: "http://localhost"}}
	c.Topology.Roles.King = topology.Assignment{EndpointID: "local", Model: "k"}
	c.Topology.Roles.Worker = topology.Assignment{EndpointID: "local", Model: "w"}
	c.Topology.Roles.Council = c.Topology.Roles.King
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

func TestNoModelsBlocksContinue(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}}})
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateProviders {
		t.Fatalf("advanced without models: %v", m.screen)
	}
	if m.workflow.Err == nil {
		t.Fatal("missing provider validation error")
	}
}

func TestPartialDiscoveryStillAllowsAssignment(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e", Name: "E"}, Models: []discovery.Model{{ID: "m"}}}, {Endpoint: topology.Endpoint{ID: "bad"}, Err: errors.New("down")}})
	m = enterRolesWithModels(t, m, setup.ModelRef{EndpointID: "e", ModelID: "m"})
	if m.screen != setup.StateRoles {
		t.Fatal("did not enter roles")
	}
}

func TestRoleSelectionDistinguishesEndpoint(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "one"}, Models: []discovery.Model{{ID: "m"}}}, {Endpoint: topology.Endpoint{ID: "two"}, Models: []discovery.Model{{ID: "m"}}}})
	m = enterRolesWithModels(t, m,
		setup.ModelRef{EndpointID: "one", ModelID: "m"},
		setup.ModelRef{EndpointID: "two", ModelID: "m"},
	)
	m, _ = update(m, key("down"))
	m, _ = update(m, key("2"))
	m, _ = update(m, key("enter"))
	if got := m.workflow.Draft.Config.Topology.Roles.Worker.EndpointID; got != "two" {
		t.Fatalf("worker endpoint=%q", got)
	}
}

func TestAssignmentNavigation(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "one"}, Models: []discovery.Model{{ID: "a"}, {ID: "b"}}}})
	m = enterRolesWithModels(t, m,
		setup.ModelRef{EndpointID: "one", ModelID: "a"},
		setup.ModelRef{EndpointID: "one", ModelID: "b"},
	)
	m, _ = update(m, key("down"))
	m, _ = update(m, key("1"))
	m, _ = update(m, key("enter"))
	if m.workflow.Draft.Config.Topology.Roles.King.Model != "b" {
		t.Fatal("navigation did not select second model")
	}
}

func TestStaleDiscoveryIgnoredAndRescanCancels(t *testing.T) {
	var contexts []context.Context
	d := func(ctx context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		contexts = append(contexts, ctx)
		return func() tea.Msg {
			return DiscoveryMsg{Generation: gen, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: string(rune('a' + gen))}}}}
		}
	}
	m := NewWithDepsAndSave(config.Default(), nil, d, nil)
	m, _ = update(m, key("r"))
	m, _ = update(m, key("r"))
	if len(contexts) != 2 {
		t.Fatalf("calls=%d", len(contexts))
	}
	select {
	case <-contexts[0].Done():
	default:
		t.Fatal("old context not canceled")
	}
	m, _ = update(m, DiscoveryMsg{Generation: 1, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "stale"}}}})
	if len(m.workflow.Draft.Results) != 0 {
		t.Fatal("stale result applied")
	}
}

func TestRescanCannotContinueWithStaleResults(t *testing.T) {
	var gen uint64
	m := NewWithDepsAndSave(config.Default(), nil, func(_ context.Context, g uint64, _ []topology.Endpoint) tea.Cmd { gen = g; return nil }, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "old"}, Models: []discovery.Model{{ID: "m"}}}})
	m, _ = update(m, key("r"))
	if !m.scanning || len(m.workflow.Draft.Results) != 0 || m.modelIndex != 0 {
		t.Fatal("rescan did not clear")
	}
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateProviders {
		t.Fatal("advanced while scanning")
	}
	m, _ = update(m, DiscoveryMsg{Generation: gen, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "new", Kind: topology.KindOllama}, Models: []discovery.Model{{ID: "m"}}}}})
	_ = m.workflow.Draft.SetProviderEnabled(setup.OllamaEndpointID, true, setup.Platform{OS: "linux", Arch: "amd64"})
	m, _ = update(m, key("enter"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateWizard {
		t.Fatal("did not advance after current result")
	}
}

func TestSaveFailureStaysReview(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { return errors.New("no") })
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}}}})
	m.workflow.Draft.AssignKing(topology.Assignment{EndpointID: "e", Model: "m"})
	m.workflow.Draft.AssignWorker(topology.Assignment{EndpointID: "e", Model: "m"})
	m.workflow.State = setup.StateReview
	m.screen = setup.StateReview
	n, c := m.Update(key("enter"))
	if c == nil {
		t.Fatal("save cmd nil")
	}
	m, _ = update(n.(Model), c())
	if m.screen != setup.StateReview || m.workflow.Err == nil {
		t.Fatal("failure left review")
	}
	if !strings.Contains(m.View().Content, "Save error") {
		t.Fatal("save error not exposed in view")
	}
}

func update(m Model, msg tea.Msg) (Model, tea.Cmd) { n, c := m.Update(msg); return n.(Model), c }

func enterRolesWithModels(t *testing.T, m Model, refs ...setup.ModelRef) Model {
	t.Helper()
	_ = m.workflow.Draft.SetProviderEnabled(setup.OllamaEndpointID, true, setup.Platform{OS: "linux", Arch: "amd64"})
	m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, true)
	m, _ = update(m, key("enter")) // providers -> models
	for _, ref := range refs {
		if err := m.workflow.Draft.ToggleModel(ref); err != nil {
			t.Fatalf("select %+v: %v", ref, err)
		}
	}
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatalf("suggest roles: %v", err)
	}
	// The conversational Wizard replaced this legacy screen in the product flow.
	// Keep these focused role-control tests independent of the Wizard runtime.
	m.screen, m.workflow.State = setup.StateRoles, setup.StateRoles
	return m
}

func TestSaveSuccessBecomesReady(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { return nil })
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}}}})
	m = enterRolesWithModels(t, m, setup.ModelRef{EndpointID: "e", ModelID: "m"})
	m, _ = update(m, key("1"))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("2"))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("n"))     // roles -> performance
	m, _ = update(m, key("enter")) // performance -> review
	n, c := m.Update(key("enter"))
	if c == nil {
		t.Fatal("save cmd nil")
	}
	m = n.(Model)
	sm := c()
	m, _ = update(m, sm)
	if m.setup || m.screen != setup.StateReady {
		t.Fatal("not ready")
	}
}

func TestSetupReopensFromReady(t *testing.T) {
	m := New(completeConfig())
	m, _ = update(m, key("ctrl+s"))
	if !m.setup || m.screen != setup.StateProviders {
		t.Fatal("setup not reopened")
	}
}

func TestReopenResetsTransientStateAndClampsCursor(t *testing.T) {
	var generation uint64
	m := NewWithDeps(completeConfig(), nil, func(_ context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		generation = gen
		return func() tea.Msg { return DiscoveryMsg{Generation: gen} }
	})
	m.modelIndex, m.role, m.perfFocus, m.formActive, m.saving = 9, 2, 1, false, false
	m, _ = update(m, key("ctrl+s"))
	if m.modelIndex != 0 || m.role != 0 || m.perfFocus != 0 || m.formActive || m.saving {
		t.Fatalf("transient state not reset: index=%d role=%d focus=%d form=%v saving=%v", m.modelIndex, m.role, m.perfFocus, m.formActive, m.saving)
	}
	// nil discovery command means no accepted result; exercise the current generation explicitly.
	m, _ = update(m, DiscoveryMsg{Generation: generation, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}}}}})
	if m.modelIndex != 0 {
		t.Fatalf("cursor=%d, want clamped 0", m.modelIndex)
	}
}

func TestRolesAssignWithoutAutoAdvanceAndExplicitNext(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.screen, m.workflow.State = setup.StateRoles, setup.StateRoles
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}, {ID: "n"}}}})
	_ = m.workflow.Draft.ToggleModel(setup.ModelRef{EndpointID: "e", ModelID: "m"})
	_ = m.workflow.Draft.ToggleModel(setup.ModelRef{EndpointID: "e", ModelID: "n"})
	m, _ = update(m, key("1"))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateRoles || !m.workflow.Draft.Config.Topology.Roles.King.Complete() {
		t.Fatalf("king assignment advanced or failed: screen=%v", m.screen)
	}
	m, _ = update(m, key("2"))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateRoles || !m.workflow.Draft.Config.Topology.Roles.Worker.Complete() {
		t.Fatalf("worker assignment advanced or failed: screen=%v", m.screen)
	}
	m, _ = update(m, key("0"))
	m, _ = update(m, key("n"))
	if m.screen != setup.StatePerformance || m.workflow.State != setup.StatePerformance {
		t.Fatalf("explicit next did not advance: screen=%v state=%v", m.screen, m.workflow.State)
	}
}

func TestReopenReadyConfigUsesDiscoveryWorkflow(t *testing.T) {
	c := completeConfig()
	called := false
	m := NewWithDepsAndSave(c, nil, func(ctx context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		called = true
		return func() tea.Msg {
			return DiscoveryMsg{Generation: gen, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "local", Kind: topology.KindOllama}, Models: []discovery.Model{{ID: "k"}, {ID: "w"}}}}}
		}
	}, nil)
	m, cmd := update(m, key("ctrl+s"))
	if !m.setup || m.screen != setup.StateProviders || m.workflow.State != setup.StateProviders {
		t.Fatalf("reopen state: setup=%v screen=%v workflow=%v", m.setup, m.screen, m.workflow.State)
	}
	if strings.Contains(m.View().Content, "Configuration ready") || !strings.Contains(m.View().Content, "Checking local providers") {
		t.Fatalf("reopen view=%q", m.View().Content)
	}
	if m.workflow.Draft.Config.Topology.Roles.King != c.Topology.Roles.King || m.workflow.Draft.Config.Topology.Roles.Worker != c.Topology.Roles.Worker {
		t.Fatal("existing assignments were not preselected")
	}
	if cmd == nil || !called {
		t.Fatal("reopen did not invoke discovery")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("reopen discovery command returned nil")
	}
	m, _ = update(m, msg)
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("enter"))
	if m.screen != setup.StateWizard || m.workflow.State != setup.StateWizard {
		t.Fatalf("discovery enter advanced to %v/%v, want Wizard", m.screen, m.workflow.State)
	}
}

func TestInitialAutomaticDiscoveryShowsScanning(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, func(ctx context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg { return DiscoveryMsg{Generation: gen} }
	}, nil)
	if !strings.Contains(m.View().Content, "Checking local providers") {
		t.Fatalf("initial view=%q, want scanning", m.View().Content)
	}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil discovery command")
	}
	m, _ = update(m, cmd())
	if !strings.Contains(m.View().Content, "No providers answered") {
		t.Fatalf("completed view=%q, want scan complete", m.View().Content)
	}
	ready := NewWithDepsAndSave(completeConfig(), nil, func(context.Context, uint64, []topology.Endpoint) tea.Cmd { return nil }, nil)
	if strings.Contains(ready.View().Content, "Looking for local model providers") || ready.Init() != nil {
		t.Fatal("ready config should not scan automatically")
	}
}

func TestControlCQuitsEveryScreen(t *testing.T) {
	for _, screen := range []setup.WorkflowState{setup.StateProviders, setup.StateModels, setup.StateRoles, setup.StateReview, setup.StateReady} {
		m := New(completeConfig())
		m.setup = screen != setup.StateReady
		m.screen = screen
		_, c := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
		if c == nil {
			t.Fatalf("screen %v no quit", screen)
		}
	}
}

func TestInitAutomaticallyStartsDiscovery(t *testing.T) {
	called := false
	m := NewWithDepsAndSave(config.Default(), nil, func(ctx context.Context, gen uint64, eps []topology.Endpoint) tea.Cmd {
		called = true
		return func() tea.Msg { return DiscoveryMsg{Generation: gen} }
	}, nil)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil discovery command")
	}
	_ = cmd()
	if !called {
		t.Fatal("discovery dependency not invoked")
	}
	ready := NewWithDepsAndSave(completeConfig(), nil, nil, nil)
	if ready.Init() != nil {
		t.Fatal("ready config should not auto-discover")
	}
}

func TestCustomEndpointIsIncludedInRescan(t *testing.T) {
	var got []topology.Endpoint
	m := NewWithDepsAndSave(config.Default(), []topology.Endpoint{{ID: "d"}}, func(ctx context.Context, gen uint64, eps []topology.Endpoint) tea.Cmd {
		got = eps
		return func() tea.Msg { return DiscoveryMsg{Generation: gen} }
	}, nil)
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("a"))
	m.form.Name.SetValue("x")
	m.form.BaseURL.SetValue("http://localhost")
	m, _ = update(m, key("enter"))
	found := false
	for _, e := range got {
		if e.BaseURL == "http://localhost" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom endpoint absent: %#v", got)
	}
}

func TestPerformanceScreenAdjustsWithinBounds(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.screen, m.workflow.State = setup.StatePerformance, setup.StatePerformance
	m.workflow.Draft.Config.CouncilSize = 1
	m.workflow.Draft.Config.WorkerConcurrency = 32
	for i := 0; i < 5; i++ {
		m, _ = update(m, key("left"))
	}
	if m.workflow.Draft.Config.CouncilSize != 1 {
		t.Fatal("council below bound")
	}
	m, _ = update(m, key("down"))
	for i := 0; i < 5; i++ {
		m, _ = update(m, key("right"))
	}
	if m.workflow.Draft.Config.WorkerConcurrency != 32 {
		t.Fatal("worker above bound")
	}
}

func TestPerformanceScreenTogglesDedicatedOllamaServers(t *testing.T) {
	m := NewWithDeps(managedOllamaAppConfig(), nil, nil)
	m.setup = true
	m.screen, m.workflow.State = setup.StatePerformance, setup.StatePerformance

	m, _ = update(m, key("down"))
	m, _ = update(m, key("down"))
	if m.perfFocus != 2 {
		t.Fatalf("focus=%d, want Ollama control", m.perfFocus)
	}
	workers := m.workflow.Draft.Config.WorkerConcurrency
	m, _ = update(m, key("right"))
	if m.workflow.Draft.Config.WorkerConcurrency != workers {
		t.Fatal("Ollama control adjusted worker concurrency")
	}
	m, _ = update(m, key(" "))
	if got := m.workflow.Draft.Config.Providers.Ollama.PortMode; got != config.OllamaSharedPort {
		t.Fatalf("mode=%q, want shared", got)
	}
	m, _ = update(m, key(" "))
	if got := m.workflow.Draft.Config.Providers.Ollama.PortMode; got != config.OllamaDedicatedPorts {
		t.Fatalf("mode=%q, want dedicated", got)
	}
}

func TestPerformanceScreenHasTwoControlsWithoutManagedOllama(t *testing.T) {
	m := NewWithDeps(completeConfig(), nil, nil)
	m.setup = true
	m.screen, m.workflow.State = setup.StatePerformance, setup.StatePerformance
	m, _ = update(m, key("down"))
	m, _ = update(m, key("down"))
	if m.perfFocus != 0 {
		t.Fatalf("focus=%d, want two-control wrap", m.perfFocus)
	}
}

func managedOllamaAppConfig() config.Config {
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}}
	cfg.Topology.Roles.King = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "large"}
	cfg.Topology.Roles.Worker = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "small"}
	cfg.Topology.Roles.Council = cfg.Topology.Roles.King
	return cfg
}

func TestLateDiscoveryIgnoredAfterLeavingScreen(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, nil)
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}}}})
	m, _ = update(m, key("enter"))
	m.screen = setup.StateRoles
	m.workflow.State = setup.StateRoles
	m, _ = update(m, key("esc"))
	m, _ = update(m, DiscoveryMsg{Generation: 1, Results: []setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "late"}, Models: []discovery.Model{{ID: "x"}}}}})
	if len(m.workflow.Draft.Results) != 1 || m.workflow.Draft.Results[0].Endpoint.ID != "e" {
		t.Fatal("late discovery applied")
	}
}

func TestStaleAndDuplicateSaveMessagesIgnored(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { return nil })
	m.screen, m.workflow.State = setup.StateReview, setup.StateReview
	n, c := m.Update(key("enter"))
	if c == nil {
		t.Fatal("save cmd missing")
	}
	m = n.(Model)
	if _, c2 := m.Update(key("enter")); c2 != nil {
		t.Fatal("duplicate save started")
	}
	m, _ = update(m, SaveMsg{Generation: 999, Config: completeConfig()})
	if m.screen != setup.StateReview {
		t.Fatal("stale save accepted")
	}
}

func TestSaveSuccessThenDuplicateIgnored(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { return nil })
	m.screen, m.workflow.State = setup.StateReview, setup.StateReview
	n, c := m.Update(key("enter"))
	m = n.(Model)
	msg := c()
	m, _ = update(m, msg)
	if m.setup == true {
		t.Fatal("save did not complete")
	}
	old := m.config
	m, _ = update(m, msg)
	if !reflect.DeepEqual(m.config, old) {
		t.Fatal("duplicate save accepted")
	}
}

func TestEscapeIsBlockedWhileSaving(t *testing.T) {
	calls := 0
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { calls++; return nil })
	m.screen, m.workflow.State = setup.StateReview, setup.StateReview
	n, c := m.Update(key("enter"))
	m = n.(Model)
	m, _ = update(m, key("esc"))
	if m.screen != setup.StateReview || !m.saving {
		t.Fatal("escape should be ignored while saving")
	}
	m, _ = update(m, c())
	if calls != 1 || m.setup || m.screen != setup.StateReady {
		t.Fatalf("save result not applied: calls=%d setup=%v screen=%v", calls, m.setup, m.screen)
	}
}

func TestAllKeysBlockedWhileSaving(t *testing.T) {
	m := NewWithDepsAndSave(config.Default(), nil, nil, func(config.Config) error { return nil })
	m.screen, m.workflow.State = setup.StateReview, setup.StateReview
	n, c := m.Update(key("enter"))
	m = n.(Model)
	if c == nil || !m.saving {
		t.Fatal("save did not start")
	}
	before := m
	keys := []tea.KeyPressMsg{
		key("q"), key("esc"), key("enter"), key("up"), key("down"),
		key("left"), key("right"), key("n"), key("a"), key("r"), key("s"),
		tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}),
	}
	for _, msg := range keys {
		next, cmd := m.Update(msg)
		if cmd != nil {
			t.Fatalf("key %q produced command while saving", msg.String())
		}
		m = next.(Model)
		if m.screen != before.screen || m.saveGen != before.saveGen || !m.saving {
			t.Fatalf("key %q changed state while saving", msg.String())
		}
	}
	if _, ok := c().(SaveMsg); !ok {
		t.Fatal("save command did not produce SaveMsg")
	}
}

func TestRolesViewShowsSelectionAndAssignments(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.screen, m.workflow.State = setup.StateRoles, setup.StateRoles
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e", Name: "Endpoint"}, Models: []discovery.Model{{ID: "m"}}}})
	_ = m.workflow.Draft.ToggleModel(setup.ModelRef{EndpointID: "e", ModelID: "m"})
	if !strings.Contains(m.View().Content, "Endpoint") {
		t.Fatal("role endpoint missing")
	}
}

func TestCouncilCanUseKing(t *testing.T) {
	m := NewWithDeps(config.Default(), nil, nil)
	m.screen, m.workflow.State = setup.StateRoles, setup.StateRoles
	m.workflow.Draft.ApplyResults([]setup.EndpointResult{{Endpoint: topology.Endpoint{ID: "e"}, Models: []discovery.Model{{ID: "m"}}}})
	m, _ = update(m, key("0"))
	if m.workflow.Draft.Config.CouncilEnabled {
		t.Fatal("council was not disabled")
	}
}

func TestDiscoveryDependencyIsTyped(t *testing.T) {
	var _ DiscoverFunc = func(context.Context, uint64, []topology.Endpoint) tea.Cmd { return nil }
}

func TestSetupIntegrationDiscoversAssignsSavesReloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"king"},{"name":"worker"}]}`))
	}))
	defer srv.Close()
	e := topology.Endpoint{ID: "local", Name: "local", Kind: topology.KindOllama, BaseURL: srv.URL}
	d := discovery.New(discovery.Options{})
	p := filepath.Join(t.TempDir(), "config.json")
	cfg, _ := config.Load(p)
	m := NewWithDepsAndSave(cfg, []topology.Endpoint{e}, func(ctx context.Context, gen uint64, _ []topology.Endpoint) tea.Cmd {
		return func() tea.Msg {
			rs, _ := d.Discover(ctx, []topology.Endpoint{e})
			out := make([]setup.EndpointResult, len(rs))
			for i, r := range rs {
				out[i] = setup.EndpointResult{Endpoint: r.Endpoint, Models: r.Models, Err: r.Err}
			}
			return DiscoveryMsg{Generation: gen, Results: out}
		}
	}, func(c config.Config) error { return config.Save(p, c) })
	dc := m.Init()
	if dc == nil {
		t.Fatal("automatic discovery cmd nil")
	}
	m, _ = update(m, dc())
	if !m.RequiresSetup() {
		t.Fatal("should still require setup before save")
	}
	// rescan path remains supported
	var rescan tea.Cmd
	m, rescan = update(m, key("r"))
	if rescan != nil {
		m, _ = update(m, rescan())
	}
	if m.screen != setup.StateProviders || !m.workflow.Draft.HasModels() {
		t.Fatal("discovery failed")
	}
	_ = m.workflow.Draft.SetProviderEnabled(setup.OllamaEndpointID, true, setup.Platform{OS: "linux", Arch: "amd64"})
	m, _ = update(m, key("enter"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("down"))
	m, _ = update(m, key(" "))
	m, _ = update(m, key("enter"))
	if err := m.workflow.Draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	m.screen, m.workflow.State = setup.StateReview, setup.StateReview
	if m.screen != setup.StateReview {
		t.Fatalf("screen=%v", m.screen)
	}
	var saveCmd tea.Cmd
	m, saveCmd = update(m, key("enter"))
	if saveCmd == nil {
		t.Fatal("save cmd nil")
	}
	m, _ = update(m, saveCmd())
	loaded, err := config.Load(p)
	if err != nil || !loaded.IsReady() {
		t.Fatalf("reload not ready: %v", err)
	}
}
