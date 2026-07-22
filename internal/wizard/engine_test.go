package wizard

import (
	"context"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type wizardChatClient struct {
	responses []string
	messages  [][]modelapi.Message
}

func (f *wizardChatClient) Chat(_ context.Context, _ topology.Endpoint, _ string, messages []modelapi.Message) (string, error) {
	f.messages = append(f.messages, append([]modelapi.Message(nil), messages...))
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestWizardStartAppliesDefaultsBeforeConciseRecommendation(t *testing.T) {
	draft := wizardDraft(t)
	client := &wizardChatClient{responses: []string{`{"type":"message","content":"I recommend the larger model for King and the smaller model for Worker.","ready":true}`}}
	engine := NewEngine(client, draft.SelectedModels()[1], NewSession(draft))
	reply, err := engine.Start(context.Background())
	if err != nil || !reply.Ready || !strings.Contains(reply.Content, "larger model") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if draft.Config.Topology.Roles.King.Model != "large" || draft.Config.Topology.Roles.Worker.Model != "small" || draft.Config.CouncilEnabled {
		t.Fatalf("defaults=%+v", draft.Config)
	}
	if len(client.messages) != 1 || !strings.Contains(client.messages[0][0].Content, "setup-only") || !strings.Contains(client.messages[0][0].Content, "assign_model") {
		t.Fatalf("system prompt=%+v", client.messages)
	}
}

func TestWizardUsesOnePurposeToolsThenExplainsUpdatedDraft(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	client := &wizardChatClient{responses: []string{
		`{"type":"tool","name":"enable_council","arguments":{"enabled":true}}`,
		`{"type":"tool","name":"assign_model","arguments":{"role":"council","model_number":2}}`,
		`{"type":"tool","name":"set_council_size","arguments":{"count":2}}`,
		`{"type":"message","content":"Council enabled with two reviewers using model 2.","ready":true}`,
	}}
	engine := NewEngine(client, draft.SelectedModels()[1], NewSession(draft))
	reply, err := engine.Respond(context.Background(), "Please add a small council")
	if err != nil || !reply.Ready || !strings.Contains(reply.Content, "two reviewers") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if !draft.Config.CouncilEnabled || draft.Config.CouncilSize != 2 || draft.Config.Topology.Roles.Council.Model != "small" {
		t.Fatalf("draft=%+v", draft.Config)
	}
}

func TestWizardRepairsMalformedControlResponseThenFallsBack(t *testing.T) {
	draft := wizardDraft(t)
	client := &wizardChatClient{responses: []string{"not json", "still not json"}}
	engine := NewEngine(client, draft.SelectedModels()[0], NewSession(draft))
	reply, err := engine.Start(context.Background())
	if err != nil || !reply.Ready || !reply.Fallback || !strings.Contains(reply.Content, "prepared") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if len(client.messages) != 2 || !strings.Contains(client.messages[1][len(client.messages[1])-1].Content, "valid JSON") {
		t.Fatalf("repair messages=%+v", client.messages)
	}
}

func TestWizardStopsRunawayToolLoop(t *testing.T) {
	draft := wizardDraft(t)
	responses := make([]string, 12)
	for index := range responses {
		responses[index] = `{"type":"tool","name":"inspect_setup","arguments":{}}`
	}
	engine := NewEngine(&wizardChatClient{responses: responses}, draft.SelectedModels()[0], NewSession(draft))
	if _, err := engine.Respond(context.Background(), "loop"); err == nil || !strings.Contains(err.Error(), "tool limit") {
		t.Fatalf("error=%v", err)
	}
}

func TestWizardModelMustBeSelected(t *testing.T) {
	draft := wizardDraft(t)
	engine := NewEngine(&wizardChatClient{}, setup.ModelOption{Ref: setup.ModelRef{EndpointID: "other", ModelID: "x"}}, NewSession(draft))
	if _, err := engine.Start(context.Background()); err == nil {
		t.Fatal("unselected Wizard model accepted")
	}
}
