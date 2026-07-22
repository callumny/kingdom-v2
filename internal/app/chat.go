package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/skills"
)

// chatEventMsg carries one orchestration event together with the run
// generation that produced it. Events from cancelled or superseded runs are
// ignored by Update.
type chatEventMsg struct {
	Generation uint64
	Event      orchestration.Event
}

func (m Model) startRunStream(ctx context.Context, cfg config.Config, prompt string, active []skills.Skill) <-chan orchestration.Event {
	if m.prepareRun == nil {
		return m.run(ctx, cfg, prompt, active)
	}
	out := make(chan orchestration.Event)
	go func() {
		defer close(out)
		emit := func(event orchestration.Event) bool {
			select {
			case out <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		runtimeConfig, warmed := m.awaitRuntimeWarmup(ctx, cfg)
		if !warmed {
			if !emit(orchestration.Event{Type: orchestration.EventRuntimePreparing}) {
				return
			}
			var err error
			runtimeConfig, err = m.prepareRun(ctx, cfg)
			if err != nil {
				emit(orchestration.Event{Type: orchestration.EventFailed, Message: err.Error()})
				return
			}
		}
		stream := m.run(ctx, runtimeConfig, prompt, active)
		if stream == nil {
			emit(orchestration.Event{Type: orchestration.EventFailed, Message: "orchestration stream unavailable"})
			return
		}
		for {
			select {
			case event, ok := <-stream:
				if !ok {
					return
				}
				if !emit(event) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (m Model) awaitRuntimeWarmup(ctx context.Context, cfg config.Config) (config.Config, bool) {
	if m.runtimeWarm == nil || m.runtimeWarmSignature != configSignature(cfg) {
		return config.Config{}, false
	}
	select {
	case result, ok := <-m.runtimeWarm:
		if !ok || result.Err != nil {
			return config.Config{}, false
		}
		return result.Config, true
	case <-ctx.Done():
		return config.Config{}, false
	}
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
