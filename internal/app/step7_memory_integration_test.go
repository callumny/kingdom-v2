package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/topology"
)

func TestConversationPersistsRecallsAndBrowsesThroughTUI(t *testing.T) {
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var mu sync.Mutex
	var requests [][]modelapi.Message
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []modelapi.Message `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		requests = append(requests, body.Messages)
		call := len(requests)
		mu.Unlock()
		content := "blue saved"
		if call == 2 {
			content = "your colour is blue"
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"message": map[string]string{"content": `{"type":"final","content":"` + content + `"}`}})
	}))
	defer server.Close()

	configuration := completeConfig()
	configuration.Topology.Endpoints = []topology.Endpoint{{ID: "local", Name: "local", Kind: topology.KindOllama, BaseURL: server.URL}}
	configuration.Topology.Roles.King = topology.Assignment{EndpointID: "local", Model: "king"}
	configuration.Topology.Roles.Worker = topology.Assignment{EndpointID: "local", Model: "worker"}
	client := modelapi.NewClient()
	m := NewWithServices(configuration, Services{
		Memory:       store,
		NewSessionID: func() (string, error) { return "integration-session", nil },
		Run: func(ctx context.Context, cfg config.Config, sessionID string, prompt string, _ []skills.Skill) <-chan orchestration.Event {
			return orchestration.NewEngine(cfg, client, orchestration.WithMemory(store, sessionID, 100)).Stream(ctx, prompt)
		},
	})

	m = submitAndFinish(t, m, "my colour is blue")
	m = submitAndFinish(t, m, "what is my colour?")

	mu.Lock()
	if len(requests) != 2 || !strings.Contains(messagesTextForIntegration(requests[1]), "my colour is blue") {
		mu.Unlock()
		t.Fatalf("second request did not contain recall: %+v", requests)
	}
	mu.Unlock()

	m, command := update(m, key("ctrl+m"))
	m, command = update(m, command())
	m, _ = update(m, command())
	view := m.View().Content
	if !strings.Contains(view, "my colour is blue") || !strings.Contains(view, "what is my colour?") || !strings.Contains(view, "your colour is blue") {
		t.Fatalf("persisted conversation missing from browser: %s", view)
	}
}

func submitAndFinish(t *testing.T, model Model, prompt string) Model {
	t.Helper()
	model.chat.SetValue(prompt)
	next, command := model.Update(key("ctrl+enter"))
	model = next.(Model)
	for command != nil && model.running {
		next, command = model.Update(command())
		model = next.(Model)
	}
	if model.running || model.chatError != "" {
		t.Fatalf("run did not finish cleanly: running=%v error=%q", model.running, model.chatError)
	}
	return model
}

func messagesTextForIntegration(messages []modelapi.Message) string {
	var values []string
	for _, message := range messages {
		values = append(values, message.Content)
	}
	return strings.Join(values, "\n")
}
