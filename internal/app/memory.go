package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/ui"
)

const (
	memorySessionLimit  = 100
	memoryExchangeLimit = 100
)

type MemoryBrowser interface {
	ListSessions(context.Context, int) ([]memory.Session, error)
	SessionExchanges(context.Context, string, int) ([]memory.Exchange, error)
	DeleteSession(context.Context, string) (bool, error)
}

type memoryState struct {
	store      MemoryBrowser
	open       bool
	loading    bool
	confirming bool
	cursor     int
	generation uint64
	sessions   []memory.Session
	exchanges  []memory.Exchange
	err        string
}

type memorySessionsMsg struct {
	generation uint64
	sessions   []memory.Session
	err        error
}

type memoryExchangesMsg struct {
	generation uint64
	sessionID  string
	exchanges  []memory.Exchange
	err        error
}

type memoryDeletedMsg struct {
	generation uint64
	sessionID  string
	deleted    bool
	err        error
}

func (m *Model) openMemory() tea.Cmd {
	m.memory.open = true
	m.memory.cursor = 0
	m.memory.confirming = false
	return m.loadMemorySessions()
}

func (m *Model) loadMemorySessions() tea.Cmd {
	m.memory.generation++
	generation := m.memory.generation
	store := m.memory.store
	m.memory.loading = true
	m.memory.err = ""
	m.memory.exchanges = nil
	return func() tea.Msg {
		sessions, err := store.ListSessions(context.Background(), memorySessionLimit)
		return memorySessionsMsg{generation: generation, sessions: sessions, err: err}
	}
}

func (m *Model) loadSelectedMemory() tea.Cmd {
	if m.memory.cursor < 0 || m.memory.cursor >= len(m.memory.sessions) {
		m.memory.loading = false
		m.memory.exchanges = nil
		return nil
	}
	m.memory.generation++
	generation := m.memory.generation
	sessionID := m.memory.sessions[m.memory.cursor].ID
	store := m.memory.store
	m.memory.loading = true
	m.memory.err = ""
	m.memory.exchanges = nil
	return func() tea.Msg {
		exchanges, err := store.SessionExchanges(context.Background(), sessionID, memoryExchangeLimit)
		return memoryExchangesMsg{generation: generation, sessionID: sessionID, exchanges: exchanges, err: err}
	}
}

func (m *Model) deleteSelectedMemory() tea.Cmd {
	if m.memory.cursor < 0 || m.memory.cursor >= len(m.memory.sessions) {
		return nil
	}
	m.memory.generation++
	generation := m.memory.generation
	sessionID := m.memory.sessions[m.memory.cursor].ID
	store := m.memory.store
	m.memory.loading = true
	m.memory.confirming = false
	m.memory.err = ""
	m.memory.exchanges = nil
	return func() tea.Msg {
		deleted, err := store.DeleteSession(context.Background(), sessionID)
		return memoryDeletedMsg{generation: generation, sessionID: sessionID, deleted: deleted, err: err}
	}
}

func (m Model) handleMemoryKey(key string) (Model, tea.Cmd) {
	if m.memory.confirming {
		switch key {
		case "y":
			return m, m.deleteSelectedMemory()
		case "n", "esc":
			m.memory.confirming = false
		}
		return m, nil
	}
	switch key {
	case "esc", "ctrl+m":
		m.memory.open = false
		m.memory.generation++
	case "r":
		return m, m.loadMemorySessions()
	case "down", "j":
		if len(m.memory.sessions) > 0 {
			m.memory.cursor = (m.memory.cursor + 1) % len(m.memory.sessions)
			return m, m.loadSelectedMemory()
		}
	case "up", "k":
		if len(m.memory.sessions) > 0 {
			m.memory.cursor = (m.memory.cursor - 1 + len(m.memory.sessions)) % len(m.memory.sessions)
			return m, m.loadSelectedMemory()
		}
	case "d":
		if len(m.memory.sessions) > 0 && !m.memory.loading {
			m.memory.confirming = true
		}
	}
	return m, nil
}

func (m Model) memoryView() tea.View {
	return ui.MemoryView(m.width, m.height, m.memory.sessions, m.memory.exchanges, m.memory.cursor, m.memory.loading, m.memory.confirming, m.memory.err)
}
