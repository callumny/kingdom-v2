package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/memory"
)

type fakeMemoryBrowser struct {
	sessions  []memory.Session
	exchanges map[string][]memory.Exchange
	listErr   error
	deleted   []string
	compacted []string
}

func (m *fakeMemoryBrowser) ListSessions(context.Context, int) ([]memory.Session, error) {
	return append([]memory.Session(nil), m.sessions...), m.listErr
}

func (m *fakeMemoryBrowser) SessionExchanges(_ context.Context, sessionID string, _ int) ([]memory.Exchange, error) {
	return append([]memory.Exchange(nil), m.exchanges[sessionID]...), nil
}

func (m *fakeMemoryBrowser) DeleteSession(_ context.Context, sessionID string) (bool, error) {
	m.deleted = append(m.deleted, sessionID)
	for index, session := range m.sessions {
		if session.ID == sessionID {
			m.sessions = append(m.sessions[:index], m.sessions[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *fakeMemoryBrowser) SessionContext(_ context.Context, sessionID string, _ int) (memory.Context, error) {
	return memory.Context{Exchanges: append([]memory.Exchange(nil), m.exchanges[sessionID]...)}, nil
}

func (m *fakeMemoryBrowser) CompactSession(_ context.Context, sessionID, summary string, through int64, _ memory.Usage) error {
	m.compacted = append(m.compacted, sessionID+":"+summary)
	return nil
}

func TestControlMOpensMemoryBrowserAndLoadsSelectedSession(t *testing.T) {
	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeMemoryBrowser{
		sessions:  []memory.Session{{ID: "session-recent", Preview: "hello", UpdatedAt: now, ExchangeCount: 1, ContextWindow: memory.DefaultContextWindow}},
		exchanges: map[string][]memory.Exchange{"session-recent": {{SessionID: "session-recent", User: "hello", Reply: "welcome"}}},
	}
	m := NewWithServices(completeConfig(), Services{Memory: store})
	m, command := update(m, key("ctrl+m"))
	if !m.memory.open || command == nil || !strings.Contains(m.View().Content, "Loading sessions") {
		t.Fatalf("memory did not start loading: %s", m.View().Content)
	}
	m, command = update(m, command())
	if command == nil {
		t.Fatal("session load did not request exchange details")
	}
	m, _ = update(m, command())
	view := m.View().Content
	if !strings.Contains(view, "SESSIONS") || !strings.Contains(view, "hello") || !strings.Contains(view, "welcome") {
		t.Fatalf("unexpected memory view: %s", view)
	}
	m.chat.SetValue("unchanged")
	m, _ = update(m, key("x"))
	if m.chat.Value() != "unchanged" {
		t.Fatal("memory browser leaked key into chat")
	}
	m, _ = update(m, key("esc"))
	if m.memory.open {
		t.Fatal("escape did not close memory browser")
	}
}

func TestNewSessionClearsTranscriptAndSelectedSessionCanResume(t *testing.T) {
	store := &fakeMemoryBrowser{
		sessions: []memory.Session{{ID: "existing", Preview: "Plan the demo", ExchangeCount: 1}},
		exchanges: map[string][]memory.Exchange{
			"existing": {{ID: 1, SessionID: "existing", User: "Plan the demo", Reply: "Start with the user journey."}},
		},
	}
	ids := []string{"initial", "fresh"}
	m := NewWithServices(completeConfig(), Services{
		Memory: store,
		NewSessionID: func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		},
	})
	m.history = []string{"You: old", "King: transcript"}
	m.chat.SetValue("/new")
	m, _ = update(m, key("ctrl+enter"))
	if m.sessionID != "fresh" || len(m.history) != 0 || !strings.Contains(m.progress, "New session") {
		t.Fatalf("new session state: id=%q history=%v progress=%q", m.sessionID, m.history, m.progress)
	}

	m.chat.SetValue("/sessions")
	m, command := update(m, key("ctrl+enter"))
	m, command = update(m, command())
	m, _ = update(m, command())
	m, _ = update(m, key("enter"))
	if m.memory.open || m.sessionID != "existing" || len(m.history) != 2 || m.history[0] != "You: Plan the demo" {
		t.Fatalf("resume state: open=%v id=%q history=%v", m.memory.open, m.sessionID, m.history)
	}
}

func TestCompactCommandSummarizesOlderTurnsAndKeepsRecentTurns(t *testing.T) {
	store := &fakeMemoryBrowser{exchanges: map[string][]memory.Exchange{
		"active": {
			{ID: 1, SessionID: "active", User: "one", Reply: "first"},
			{ID: 2, SessionID: "active", User: "two", Reply: "second"},
			{ID: 3, SessionID: "active", User: "three", Reply: "third"},
			{ID: 4, SessionID: "active", User: "four", Reply: "fourth"},
		},
	}}
	var compacted memory.Context
	m := NewWithServices(completeConfig(), Services{
		Memory:       store,
		NewSessionID: func() (string, error) { return "active", nil },
		Compact: func(_ context.Context, _ config.Config, next memory.Context) (string, memory.Usage, error) {
			compacted = next
			return "The first two turns established the plan.", memory.Usage{PromptTokens: 20, CompletionTokens: 8}, nil
		},
	})
	m.chat.SetValue("/compact")
	m, command := update(m, key("ctrl+enter"))
	if command == nil || !m.compacting {
		t.Fatalf("compaction did not start: %+v", m)
	}
	m, _ = update(m, command())
	if m.compacting || len(compacted.Exchanges) != 2 || len(store.compacted) != 1 || !strings.Contains(m.progress, "compacted") {
		t.Fatalf("compaction state=%+v request=%+v saved=%v", m, compacted, store.compacted)
	}
}

func TestMemoryNavigationIgnoresStaleDetailAndDeleteRequiresConfirmation(t *testing.T) {
	store := &fakeMemoryBrowser{
		sessions: []memory.Session{{ID: "new", Preview: "new question", ExchangeCount: 1}, {ID: "old", Preview: "old question", ExchangeCount: 1}},
		exchanges: map[string][]memory.Exchange{
			"new": {{SessionID: "new", User: "new question", Reply: "new answer"}},
			"old": {{SessionID: "old", User: "old question", Reply: "old answer"}},
		},
	}
	m := NewWithServices(completeConfig(), Services{Memory: store})
	m, listCommand := update(m, key("ctrl+m"))
	m, firstDetailCommand := update(m, listCommand())
	m, oldDetailCommand := update(m, key("down"))
	m, _ = update(m, firstDetailCommand())
	if strings.Contains(m.View().Content, "new answer") {
		t.Fatal("stale detail result replaced current selection")
	}
	m, _ = update(m, oldDetailCommand())
	if !strings.Contains(m.View().Content, "old answer") {
		t.Fatalf("selected detail missing: %s", m.View().Content)
	}

	m, _ = update(m, key("d"))
	if len(store.deleted) != 0 || !strings.Contains(m.View().Content, "Delete this session permanently?") {
		t.Fatal("delete was not gated by confirmation")
	}
	m, _ = update(m, key("n"))
	if len(store.deleted) != 0 {
		t.Fatal("cancelled deletion reached store")
	}
	m, _ = update(m, key("d"))
	m, deleteCommand := update(m, key("y"))
	if deleteCommand == nil {
		t.Fatal("confirmed deletion did not return command")
	}
	m, reloadCommand := update(m, deleteCommand())
	if len(store.deleted) != 1 || store.deleted[0] != "old" || reloadCommand == nil {
		t.Fatalf("delete state=%+v", store.deleted)
	}
	m, _ = update(m, reloadCommand())
	if strings.Contains(m.View().Content, "old question") || !strings.Contains(m.View().Content, "new") {
		t.Fatalf("deleted session remained visible: %s", m.View().Content)
	}
}

func TestDeletingTheActiveSessionStartsAReplacement(t *testing.T) {
	store := &fakeMemoryBrowser{
		sessions:  []memory.Session{{ID: "active", Preview: "active conversation", ExchangeCount: 1}},
		exchanges: map[string][]memory.Exchange{"active": {{ID: 1, SessionID: "active", User: "hello", Reply: "welcome"}}},
	}
	ids := []string{"active", "replacement"}
	m := NewWithServices(completeConfig(), Services{Memory: store, NewSessionID: func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}})
	m.history = []string{"You: hello", "King: welcome"}
	m, list := update(m, key("ctrl+m"))
	m, detail := update(m, list())
	m, _ = update(m, detail())
	m, _ = update(m, key("d"))
	m, remove := update(m, key("y"))
	m, reload := update(m, remove())
	if m.sessionID != "replacement" || len(m.history) != 0 || reload == nil {
		t.Fatalf("replacement state: id=%q history=%v reload=%v", m.sessionID, m.history, reload)
	}
}

func TestMemoryBrowserShowsLoadErrorsAndCannotOpenDuringRunOrSetup(t *testing.T) {
	store := &fakeMemoryBrowser{listErr: errors.New("database busy")}
	m := NewWithServices(completeConfig(), Services{Memory: store})
	m, command := update(m, key("ctrl+m"))
	m, _ = update(m, command())
	if !strings.Contains(m.View().Content, "database busy") {
		t.Fatalf("memory error missing: %s", m.View().Content)
	}

	m = NewWithServices(completeConfig(), Services{Memory: store})
	m.running = true
	m, _ = update(m, key("ctrl+m"))
	if m.memory.open {
		t.Fatal("memory opened during run")
	}
	m = NewWithServices(config.Default(), Services{Memory: store})
	m, _ = update(m, key("ctrl+m"))
	if m.memory.open {
		t.Fatal("memory opened during setup")
	}
}
