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

func TestControlMOpensMemoryBrowserAndLoadsSelectedSession(t *testing.T) {
	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeMemoryBrowser{
		sessions:  []memory.Session{{ID: "session-recent", UpdatedAt: now, ExchangeCount: 1}},
		exchanges: map[string][]memory.Exchange{"session-recent": {{SessionID: "session-recent", User: "hello", Reply: "welcome"}}},
	}
	m := NewWithServices(completeConfig(), Services{Memory: store})
	m, command := update(m, key("ctrl+m"))
	if !m.memory.open || command == nil || !strings.Contains(m.View().Content, "Loading memory") {
		t.Fatalf("memory did not start loading: %s", m.View().Content)
	}
	m, command = update(m, command())
	if command == nil {
		t.Fatal("session load did not request exchange details")
	}
	m, _ = update(m, command())
	view := m.View().Content
	if !strings.Contains(view, "Kingdom Memory") || !strings.Contains(view, "session-recent") || !strings.Contains(view, "hello") || !strings.Contains(view, "welcome") {
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

func TestMemoryNavigationIgnoresStaleDetailAndDeleteRequiresConfirmation(t *testing.T) {
	store := &fakeMemoryBrowser{
		sessions: []memory.Session{{ID: "new", ExchangeCount: 1}, {ID: "old", ExchangeCount: 1}},
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
	if strings.Contains(m.View().Content, "new question") {
		t.Fatal("stale detail result replaced current selection")
	}
	m, _ = update(m, oldDetailCommand())
	if !strings.Contains(m.View().Content, "old question") {
		t.Fatalf("selected detail missing: %s", m.View().Content)
	}

	m, _ = update(m, key("d"))
	if len(store.deleted) != 0 || !strings.Contains(m.View().Content, "Delete this session?") {
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
