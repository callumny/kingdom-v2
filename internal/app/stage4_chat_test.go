package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/topology"
)

func TestReadyQTypesButSetupQQuits(t *testing.T) {
	m, _ := update(New(completeConfig()), key("q"))
	if !strings.Contains(m.chat.Value(), "q") {
		t.Fatalf("ready q not typed: %q", m.chat.Value())
	}
	if _, cmd := New(config.Default()).Update(key("q")); cmd == nil {
		t.Fatal("setup q did not quit")
	}
}

func TestBlankSubmitDoesNotRun(t *testing.T) {
	runs := 0
	m := NewWithServices(completeConfig(), Services{Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event {
		runs++
		return nil
	}})
	m, _ = update(m, key("ctrl+enter"))
	if runs != 0 || m.running {
		t.Fatalf("blank submit started run: %d", runs)
	}
}

func TestSubmitCapturesAndClearsPrompt(t *testing.T) {
	called := make(chan string, 1)
	ch := make(chan orchestration.Event, 1)
	ch <- orchestration.Event{Type: orchestration.EventCompleted, Result: &orchestration.Result{Content: "ok"}}
	m := NewWithServices(completeConfig(), Services{Run: func(_ context.Context, _ config.Config, p string, _ []skills.Skill) <-chan orchestration.Event {
		called <- p
		return ch
	}})
	m.chat.SetValue("hello")
	n, cmd := m.Update(key("ctrl+enter"))
	m = n.(Model)
	if m.chat.Value() != "" {
		t.Fatalf("prompt not cleared: %q", m.chat.Value())
	}
	if cmd == nil || <-called != "hello" {
		t.Fatal("run did not capture prompt")
	}
}

func TestProgressEventsAndCompletion(t *testing.T) {
	ch := make(chan orchestration.Event, 2)
	ch <- orchestration.Event{Type: orchestration.EventStarted}
	ch <- orchestration.Event{Type: orchestration.EventCompleted, Result: &orchestration.Result{Content: "done"}}
	m := NewWithServices(completeConfig(), Services{Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event { return ch }})
	m.chat.SetValue("x")
	n, cmd := m.Update(key("ctrl+enter"))
	m = n.(Model)
	m, _ = update(m, cmd())
	if !strings.Contains(m.progress, "Started") {
		t.Fatal("missing progress")
	}
	m, cmd = update(m, m.nextEvent()())
	if m.running || !strings.Contains(m.history[len(m.history)-1], "done") || cmd != nil {
		t.Fatalf("completion not handled")
	}
}

func TestFailureAndUnexpectedClose(t *testing.T) {
	for _, stream := range []<-chan orchestration.Event{func() <-chan orchestration.Event {
		c := make(chan orchestration.Event, 1)
		c <- orchestration.Event{Type: orchestration.EventFailed, Message: "bad"}
		close(c)
		return c
	}(), func() <-chan orchestration.Event { c := make(chan orchestration.Event); close(c); return c }()} {
		m := NewWithServices(completeConfig(), Services{Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event { return stream }})
		m.chat.SetValue("x")
		n, cmd := m.Update(key("ctrl+enter"))
		m = n.(Model)
		if cmd == nil {
			t.Fatal("nil cmd")
		}
		m, _ = update(m, cmd())
		if m.chatError == "" {
			t.Fatal("missing failure")
		}
	}
}

func TestStaleRunGenerationIgnored(t *testing.T) {
	m := New(completeConfig())
	m.running = true
	m.runGen = 2
	m.progress = "new"
	m, _ = update(m, chatEventMsg{Generation: 1, Event: orchestration.Event{Type: orchestration.EventStarted}})
	if m.progress != "new" {
		t.Fatal("stale event applied")
	}
}

func TestEscapeCancelsActiveRun(t *testing.T) {
	cancelled := false
	m := New(completeConfig())
	m.running = true
	m.runCancel = func() { cancelled = true }
	m, _ = update(m, key("esc"))
	if !cancelled || m.running || !strings.Contains(m.progress, "Cancelled") {
		t.Fatal("escape did not cancel")
	}
}

func TestControlCCancelsAndQuits(t *testing.T) {
	cancelled := false
	m := New(completeConfig())
	m.running = true
	m.runCancel = func() { cancelled = true }
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if !cancelled || cmd == nil {
		t.Fatal("ctrl-c did not cancel/quit")
	}
}

func TestSubmitAndSetupBlockedWhileRunning(t *testing.T) {
	m := New(completeConfig())
	m.running = true
	m.chat.SetValue("x")
	m, _ = update(m, key("ctrl+enter"))
	if m.chat.Value() != "x" {
		t.Fatal("submit changed while running")
	}
	m, _ = update(m, key("ctrl+s"))
	if m.setup {
		t.Fatal("setup reopened while running")
	}
}

func TestControlSReopensSetupWhenIdle(t *testing.T) {
	m, _ := update(New(completeConfig()), key("ctrl+s"))
	if !m.setup {
		t.Fatal("setup not reopened")
	}
}
func TestNilRunOrNilStreamFailsCleanly(t *testing.T) {
	m := NewWithServices(completeConfig(), Services{})
	m.chat.SetValue("x")
	m, _ = update(m, key("ctrl+enter"))
	if m.chatError == "" || m.chat.Value() != "x" {
		t.Fatal("nil run not reported")
	}
	m = NewWithServices(completeConfig(), Services{Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event { return nil }})
	m.chat.SetValue("x")
	m, _ = update(m, key("ctrl+enter"))
	if m.chatError == "" || m.chat.Value() != "x" {
		t.Fatal("nil stream not reported")
	}
}
func TestRunUsesLatestSavedConfig(t *testing.T) {
	c := completeConfig()
	var got config.Config
	m := NewWithServices(c, Services{Run: func(_ context.Context, cfg config.Config, _ string, _ []skills.Skill) <-chan orchestration.Event {
		got = cfg
		return nil
	}})
	m.config.Topology.Roles.King.Model = "latest"
	m.chat.SetValue("x")
	m, _ = update(m, key("ctrl+enter"))
	if got.Topology.Roles.King.Model != "latest" {
		t.Fatal("run did not use latest config")
	}
}

func TestRunPreparationTransformsRuntimeConfig(t *testing.T) {
	cfg := completeConfig()
	prepared := make(chan config.Config, 1)
	m := NewWithServices(cfg, Services{
		PrepareRun: func(_ context.Context, next config.Config) (config.Config, error) {
			next.Topology.Roles.King.Model = "runtime-king"
			return next, nil
		},
		Run: func(_ context.Context, next config.Config, _ string, _ []skills.Skill) <-chan orchestration.Event {
			prepared <- next
			ch := make(chan orchestration.Event, 1)
			ch <- orchestration.Event{Type: orchestration.EventCompleted, Result: &orchestration.Result{Content: "ok"}}
			close(ch)
			return ch
		},
	})
	m.chat.SetValue("hello")
	n, cmd := m.Update(key("ctrl+enter"))
	m = n.(Model)
	if cmd == nil {
		t.Fatal("missing runtime preparation event")
	}
	m, cmd = update(m, cmd())
	if m.progress != "Starting local model servers…" {
		t.Fatalf("progress=%q", m.progress)
	}
	if cmd == nil {
		t.Fatal("preparation did not continue to orchestration")
	}
	m, _ = update(m, cmd())
	if got := (<-prepared).Topology.Roles.King.Model; got != "runtime-king" {
		t.Fatalf("runtime model=%q", got)
	}
	if cfg.Topology.Roles.King.Model == "runtime-king" {
		t.Fatal("preparation mutated persisted config")
	}
}

func TestRunPreparationFailureDoesNotStartOrchestration(t *testing.T) {
	runs := 0
	m := NewWithServices(completeConfig(), Services{
		PrepareRun: func(context.Context, config.Config) (config.Config, error) {
			return config.Config{}, errors.New("could not start Ollama")
		},
		Run: func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event {
			runs++
			return nil
		},
	})
	m.chat.SetValue("hello")
	n, cmd := m.Update(key("ctrl+enter"))
	m = n.(Model)
	m, cmd = update(m, cmd())
	if cmd == nil {
		t.Fatal("missing preparation failure event")
	}
	m, _ = update(m, cmd())
	if runs != 0 || m.running || !strings.Contains(m.chatError, "could not start Ollama") {
		t.Fatalf("runs=%d running=%v error=%q", runs, m.running, m.chatError)
	}
}

func TestChatIntegrationDelegatesWorkersCouncilAndKing(t *testing.T) {
	var calls []string
	var callsMu sync.Mutex
	kingN := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string             `json:"model"`
			Messages []modelapi.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		system := ""
		if len(req.Messages) > 0 {
			system = req.Messages[0].Content
		}
		callsMu.Lock()
		calls = append(calls, req.Model+":"+system)
		callsMu.Unlock()
		content := "worker answer"
		if strings.Contains(system, "King") {
			kingN++
			if kingN == 1 {
				content = `{"type":"delegate","tasks":[{"id":"t1","prompt":"solve"}]}`
			} else {
				content = `{"type":"final","content":"king final"}`
			}
		}
		if strings.Contains(system, "Council") {
			content = "council review"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": content}})
	}))
	defer srv.Close()
	cfg := completeConfig()
	cfg.Topology.Endpoints = []topology.Endpoint{{ID: "e", Name: "e", Kind: topology.KindOllama, BaseURL: srv.URL}}
	cfg.Topology.Roles.King.EndpointID = "e"
	cfg.Topology.Roles.Worker.EndpointID = "e"
	cfg.Topology.Roles.King.Model = "king"
	cfg.Topology.Roles.Worker.Model = "worker"
	cfg.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "council"}
	cfg.CouncilSize = 1
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	client := modelapi.NewClient()
	m := NewWithServices(cfg, Services{Run: func(ctx context.Context, c config.Config, p string, _ []skills.Skill) <-chan orchestration.Event {
		return orchestration.NewEngine(c, client).Stream(ctx, p)
	}})
	m.chat.SetValue("hello")
	n, cmd := m.Update(key("ctrl+enter"))
	m = n.(Model)
	for cmd != nil && m.running {
		msg := cmd()
		n, cmd = m.Update(msg)
		m = n.(Model)
	}
	if !strings.Contains(m.View().Content, "king final") {
		t.Fatalf("final view missing: %s", m.View().Content)
	}
	callsMu.Lock()
	got := append([]string(nil), calls...)
	callsMu.Unlock()
	want := []string{"king:You are the King. Respond with JSON action.", "worker:You are a Worker. Solve the assigned task.", "council:You are a Council reviewer. Review worker outcomes.", "king:You are the King. Respond with JSON action."}
	if len(got) != len(want) {
		t.Fatalf("unexpected role calls: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call %d=%q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("unexpected role calls: %v", calls)
	}
}
