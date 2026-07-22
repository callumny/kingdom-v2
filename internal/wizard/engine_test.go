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

type structuredWizardClient struct {
	wizardChatClient
	jsonCalls int
}

func (f *structuredWizardClient) ChatJSON(ctx context.Context, endpoint topology.Endpoint, model string, messages []modelapi.Message) (string, error) {
	f.jsonCalls++
	return f.Chat(ctx, endpoint, model, messages)
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

func TestWizardUsesStructuredOutputWhenTheProviderSupportsIt(t *testing.T) {
	draft := wizardDraft(t)
	client := &structuredWizardClient{wizardChatClient: wizardChatClient{responses: []string{
		`{"type":"message","content":"Setup ready.","ready":true}`,
	}}}
	_, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Start(context.Background())
	if err != nil || client.jsonCalls != 1 {
		t.Fatalf("structured calls=%d err=%v", client.jsonCalls, err)
	}
}

func TestWizardSwapsRoleModelsWithOneAtomicTool(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	before := draft.Config.Topology.Roles
	client := &wizardChatClient{responses: []string{
		`{"type":"tool","name":"swap_roles","arguments":{"first":"king","second":"worker"}}`,
		`{"type":"message","content":"Done.","ready":true}`,
	}}
	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), "Swap the King and Worker models")
	if err != nil || !reply.Ready || reply.Content != "Role models swapped." {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	after := draft.Config.Topology.Roles
	if after.King != before.Worker || after.Worker != before.King {
		t.Fatalf("roles were not swapped: before=%+v after=%+v", before, after)
	}
}

func TestWizardRequiresAnAssignmentToolBeforeClaimingAModelChanged(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	client := &wizardChatClient{responses: []string{
		`{"type":"message","content":"Done.","ready":true}`,
		`{"type":"tool","name":"assign_model","arguments":{"role":"king","model_number":2}}`,
		`{"type":"message","content":"Done.","ready":true}`,
	}}
	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), "Assign model 2 to the King")
	if err != nil || !reply.Ready || reply.Content != "Role assignments updated." {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if draft.Config.Topology.Roles.King.Model != "small" || len(client.messages) != 3 {
		t.Fatalf("assignment=%+v calls=%d", draft.Config.Topology.Roles.King, len(client.messages))
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
	if err != nil || !reply.Ready || !strings.Contains(reply.Content, "Council members set to 2") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if !draft.Config.CouncilEnabled || draft.Config.CouncilSize != 2 || draft.Config.Topology.Roles.Council.Model != "small" {
		t.Fatalf("draft=%+v", draft.Config)
	}
}

func TestWizardCarriesTheWholeRequestAcrossToolCalls(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	request := "Could you enable the council and bring the workers down to 2"
	client := &wizardChatClient{responses: []string{
		`{"type":"tool","name":"set_worker_concurrency","arguments":{"count":2}}`,
		`{"type":"tool","name":"enable_council","arguments":{"enabled":true}}`,
		`{"type":"message","content":"Council enabled and Worker concurrency set to 2.","ready":true}`,
	}}

	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), request)
	if err != nil || !reply.Ready {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if !draft.Config.CouncilEnabled || !draft.Config.Topology.Roles.Council.Complete() || draft.Config.WorkerConcurrency != 2 {
		t.Fatalf("multi-part request was not applied: %+v", draft.Config)
	}
	if len(client.messages) < 2 {
		t.Fatalf("messages=%+v", client.messages)
	}
	followUp := client.messages[1][len(client.messages[1])-1].Content
	for _, want := range []string{request, "every requested change"} {
		if !strings.Contains(followUp, want) {
			t.Fatalf("tool continuation missing %q: %s", want, followUp)
		}
	}
}

func TestWizardRejectsASummaryThatOmitsAnExplicitRequestedSetting(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	request := "Could you enable the council and bring the workers down to 2"
	client := &wizardChatClient{responses: []string{
		`{"type":"tool","name":"set_worker_concurrency","arguments":{"count":2}}`,
		`{"type":"message","content":"Worker concurrency set to 2.","ready":true}`,
		`{"type":"tool","name":"enable_council","arguments":{"enabled":true}}`,
		`{"type":"message","content":"Council enabled and Worker concurrency set to 2.","ready":true}`,
	}}

	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), request)
	if err != nil || !reply.Ready || !strings.Contains(reply.Content, "Council enabled") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if !draft.Config.CouncilEnabled || draft.Config.WorkerConcurrency != 2 {
		t.Fatalf("premature summary lost a requested change: %+v", draft.Config)
	}
	if len(client.messages) != 4 {
		t.Fatalf("model calls=%d, want correction and retry", len(client.messages))
	}
	correction := client.messages[2][len(client.messages[2])-1].Content
	if !strings.Contains(correction, "enable_council") {
		t.Fatalf("correction did not identify missing setting: %s", correction)
	}
}

func TestWizardCanAnswerSetupQuestionsWithoutChangingSettings(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	client := &wizardChatClient{responses: []string{
		`{"type":"message","content":"A Council adds review, while more Workers increase parallel task capacity.","ready":true}`,
	}}

	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), "Should I enable the council if I have several workers?")
	if err != nil || !reply.Ready || !strings.Contains(reply.Content, "review") {
		t.Fatalf("question was treated as a change: reply=%+v err=%v", reply, err)
	}
	if draft.Config.CouncilEnabled {
		t.Fatalf("answering a question changed setup: %+v", draft.Config)
	}
}

func TestWizardRepairsMalformedControlResponseThenFallsBack(t *testing.T) {
	draft := wizardDraft(t)
	client := &wizardChatClient{responses: []string{"not json", "still not json"}}
	engine := NewEngine(client, draft.SelectedModels()[0], NewSession(draft))
	reply, err := engine.Start(context.Background())
	if err != nil || !reply.Ready || !reply.Fallback || !strings.Contains(reply.Content, "couldn't reliably interpret") || !strings.Contains(reply.Content, "Manual setup") {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if strings.Contains(reply.Content, "I prepared a sensible setup") {
		t.Fatalf("fallback made a misleading success claim: %+v", reply)
	}
	if len(client.messages) != 2 || !strings.Contains(client.messages[1][len(client.messages[1])-1].Content, "valid JSON") {
		t.Fatalf("repair messages=%+v", client.messages)
	}
}

func TestWizardReportsTheDraftInsteadOfRepeatingModelSuccessClaims(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	client := &wizardChatClient{responses: []string{
		`{"type":"tool","name":"set_worker_concurrency","arguments":{"count":5}}`,
		`{"type":"message","content":"All changes have been successfully applied.","ready":true}`,
	}}

	reply, err := NewEngine(client, draft.SelectedModels()[1], NewSession(draft)).Respond(context.Background(), "Increase concurrent workers to 5")
	if err != nil || !reply.Ready {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if reply.Content != "Concurrent workers set to 5." {
		t.Fatalf("reply=%q, want state-derived confirmation", reply.Content)
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
