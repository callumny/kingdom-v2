package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/topology"
)

type fakeMemory struct {
	exchanges []memory.Exchange
	recallErr error
	saveErr   error
	saves     []memory.Exchange
}

func (m *fakeMemory) RecentExchanges(context.Context, int) ([]memory.Exchange, error) {
	return append([]memory.Exchange(nil), m.exchanges...), m.recallErr
}

func (m *fakeMemory) SaveExchange(_ context.Context, sessionID, user, reply string) error {
	m.saves = append(m.saves, memory.Exchange{SessionID: sessionID, User: user, Reply: reply})
	return m.saveErr
}

type memoryPromptClient struct {
	mu       sync.Mutex
	messages map[string][][]modelapi.Message
}

func (c *memoryPromptClient) Chat(_ context.Context, _ topology.Endpoint, model string, messages []modelapi.Message) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[model] = append(c.messages[model], append([]modelapi.Message(nil), messages...))
	switch model {
	case "king":
		if len(c.messages[model]) == 1 {
			return `{"type":"delegate","tasks":[{"id":"one","prompt":"investigate"}]}`, nil
		}
		return `{"type":"final","content":"remembered answer"}`, nil
	case "council":
		return "review", nil
	default:
		return "worker result", nil
	}
}

func TestMemoryRecallReachesKingOnlyAndFinalIsSaved(t *testing.T) {
	configuration := cfg()
	configuration.Topology.Roles.King.Model = "king"
	configuration.Topology.Roles.Worker.Model = "worker"
	configuration.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "council"}
	store := &fakeMemory{exchanges: []memory.Exchange{{SessionID: "earlier", User: "my colour is blue", Reply: "noted"}}}
	client := &memoryPromptClient{messages: make(map[string][][]modelapi.Message)}

	var events []Event
	for event := range NewEngine(configuration, client, WithMemory(store, "current-session", 6)).Stream(context.Background(), "what is my colour?") {
		events = append(events, event)
	}

	if len(store.saves) != 1 || store.saves[0].SessionID != "current-session" || store.saves[0].User != "what is my colour?" || store.saves[0].Reply != "remembered answer" {
		t.Fatalf("unexpected saves: %+v", store.saves)
	}
	if !hasEvent(events, EventMemoryRecall) {
		t.Fatalf("missing recall event: %+v", events)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, call := range client.messages["king"] {
		joined := messagesText(call)
		if !strings.Contains(joined, "my colour is blue") || !strings.Contains(joined, "untrusted historical context") {
			t.Fatalf("King did not receive bounded recall: %q", joined)
		}
	}
	for _, role := range []string{"worker", "council"} {
		if strings.Contains(messagesText(client.messages[role][0]), "my colour is blue") {
			t.Fatalf("%s received recalled memory", role)
		}
	}
}

func TestMemoryFailuresAreWarningsAndDoNotBreakResponse(t *testing.T) {
	store := &fakeMemory{recallErr: errors.New("recall unavailable"), saveErr: errors.New("save unavailable")}
	var events []Event
	for event := range NewEngine(cfg(), &fake{responses: []string{`{"type":"final","content":"done"}`}}, WithMemory(store, "session", 6)).Stream(context.Background(), "prompt") {
		events = append(events, event)
	}

	if events[len(events)-1].Type != EventCompleted || events[len(events)-1].Result.Content != "done" {
		t.Fatalf("response did not complete: %+v", events)
	}
	warnings := 0
	for _, event := range events {
		if event.Type == EventMemoryWarning {
			warnings++
		}
	}
	if warnings != 2 {
		t.Fatalf("warnings=%d events=%+v", warnings, events)
	}
}

func TestFailedRunIsNotSavedToMemory(t *testing.T) {
	store := &fakeMemory{}
	for range NewEngine(cfg(), nil, WithMemory(store, "session", 6)).Stream(context.Background(), "prompt") {
	}
	if len(store.saves) != 0 {
		t.Fatalf("failed run was saved: %+v", store.saves)
	}
}

func TestPlainTextResponseIsSavedToMemory(t *testing.T) {
	store := &fakeMemory{}
	client := &fake{responses: []string{"a normal reply"}}
	for range NewEngine(cfg(), client, WithMemory(store, "session", 6)).Stream(context.Background(), "prompt") {
	}
	if len(store.saves) != 1 || store.saves[0].Reply != "a normal reply" {
		t.Fatalf("plain response was not saved: %+v", store.saves)
	}
}

func hasEvent(events []Event, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func messagesText(messages []modelapi.Message) string {
	var parts []string
	for _, message := range messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}
