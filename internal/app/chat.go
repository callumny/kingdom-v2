package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/orchestration"
)

// chatEventMsg carries one orchestration event together with the run
// generation that produced it. Events from cancelled or superseded runs are
// ignored by Update.
type chatEventMsg struct {
	Generation uint64
	Event      orchestration.Event
}

// nextEvent returns a command that waits for the next event from the active
// orchestration stream. A closed stream is surfaced as a deterministic
// failure, avoiding a UI that remains stuck in the running state.
func (m *Model) nextEvent() tea.Cmd {
	if !m.running || m.runCh == nil {
		return nil
	}
	gen := m.runGen
	ch := m.runCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return chatEventMsg{Generation: gen, Event: orchestration.Event{Type: orchestration.EventFailed, Message: "orchestration stream closed unexpectedly"}}
		}
		return chatEventMsg{Generation: gen, Event: ev}
	}
}
