package app

import (
	"context"
	"errors"
	"fmt"

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
	SessionContext(context.Context, string, int) (memory.Context, error)
	CompactSession(context.Context, string, string, int64, memory.Usage) error
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

type sessionCompactedMsg struct {
	generation uint64
	sessionID  string
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
	case "enter":
		if !m.memory.loading {
			m.resumeSelectedSession()
		}
	case "n":
		return m.startNewSession()
	case "c":
		if len(m.memory.sessions) > 0 && !m.memory.loading {
			return m.startSessionCompaction(m.memory.sessions[m.memory.cursor].ID)
		}
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
	return ui.MemoryView(m.width, m.height, m.memory.sessions, m.memory.exchanges, m.memory.cursor, m.sessionID, m.memory.loading, m.memory.confirming, m.compacting, m.memory.err)
}

func (m *Model) resumeSelectedSession() {
	if m.memory.cursor < 0 || m.memory.cursor >= len(m.memory.sessions) {
		return
	}
	m.sessionID = m.memory.sessions[m.memory.cursor].ID
	m.history = transcriptHistory(m.memory.exchanges)
	m.progress = "Session resumed"
	m.chatError = ""
	m.memory.open = false
	m.memory.generation++
}

func (m Model) startNewSession() (Model, tea.Cmd) {
	if m.newSessionID == nil {
		m.chatError = "session manager is unavailable"
		return m, nil
	}
	sessionID, err := m.newSessionID()
	if err != nil {
		m.chatError = "start session: " + err.Error()
		return m, nil
	}
	m.sessionID = sessionID
	m.history = nil
	m.progress = "New session started"
	m.chatError = ""
	m.memory.open = false
	m.memory.generation++
	return m, nil
}

func transcriptHistory(exchanges []memory.Exchange) []string {
	history := make([]string, 0, len(exchanges)*2)
	for _, exchange := range exchanges {
		history = append(history, "You: "+exchange.User, "King: "+exchange.Reply)
	}
	return history
}

func (m Model) startSessionCompaction(sessionID string) (Model, tea.Cmd) {
	if m.memory.store == nil || m.compact == nil {
		m.chatError = "session compaction is unavailable"
		return m, nil
	}
	if m.compactCancel != nil {
		m.compactCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.compactCancel = cancel
	m.compactGeneration++
	generation := m.compactGeneration
	store := m.memory.store
	compactor := m.compact
	configuration := m.config
	m.compacting = true
	m.progress = "Compacting session…"
	m.chatError = ""
	return m, func() tea.Msg {
		sessionContext, err := store.SessionContext(ctx, sessionID, memoryExchangeLimit)
		if err != nil {
			return sessionCompactedMsg{generation: generation, sessionID: sessionID, err: err}
		}
		if len(sessionContext.Exchanges) <= 2 {
			return sessionCompactedMsg{generation: generation, sessionID: sessionID, err: errors.New("session needs more than two exchanges before compaction")}
		}
		older := append([]memory.Exchange(nil), sessionContext.Exchanges[:len(sessionContext.Exchanges)-2]...)
		request := memory.Context{Summary: sessionContext.Summary, Exchanges: older}
		summary, usage, err := compactor(ctx, configuration, request)
		if err == nil {
			if summary == "" {
				err = errors.New("compactor returned an empty summary")
			} else {
				err = store.CompactSession(ctx, sessionID, summary, older[len(older)-1].ID, usage)
			}
		}
		if err != nil && ctx.Err() != nil {
			err = fmt.Errorf("compaction cancelled: %w", ctx.Err())
		}
		return sessionCompactedMsg{generation: generation, sessionID: sessionID, err: err}
	}
}
