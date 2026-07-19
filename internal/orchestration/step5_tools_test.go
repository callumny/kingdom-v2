package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
)

type recordingToolRunner struct {
	mu     sync.Mutex
	calls  []tools.Call
	output string
}

func (r *recordingToolRunner) Run(ctx context.Context, call tools.Call, approver tools.Approver) tools.Result {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	if call.Name == "write_file" {
		approved, err := approver.Approve(ctx, tools.Approval{Call: call, Summary: "notes.txt", Risk: "write"})
		if err != nil {
			return tools.Result{ID: call.ID, Name: call.Name, Error: err.Error()}
		}
		if !approved {
			return tools.Result{ID: call.ID, Name: call.Name, Error: "denied", Denied: true}
		}
	}
	output := r.output
	if output == "" {
		output = "tool output"
	}
	return tools.Result{ID: call.ID, Name: call.Name, Output: output}
}

func (r *recordingToolRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

type messageRecordingClient struct {
	mu        sync.Mutex
	responses []string
	messages  [][]modelapi.Message
}

func (c *messageRecordingClient) Chat(_ context.Context, _ topology.Endpoint, _ string, messages []modelapi.Message) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, append([]modelapi.Message(nil), messages...))
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestToolActionPausesForApprovalAndReturnsResultToKing(t *testing.T) {
	client := &messageRecordingClient{responses: []string{
		`{"type":"tool","tool":{"id":"call-1","name":"write_file","arguments":{"path":"notes.txt","content":"hello"}}}`,
		`{"type":"final","content":"done"}`,
	}}
	runner := &recordingToolRunner{}
	var events []EventType
	for event := range NewEngine(cfg(), client, WithTools(runner)).Stream(context.Background(), "create notes") {
		events = append(events, event.Type)
		if event.Type == EventToolApproval {
			if event.Approval == nil || event.Approval.Approval().Call.ID != "call-1" {
				t.Fatalf("bad approval event: %+v", event)
			}
			if !event.Approval.Resolve(true) || event.Approval.Resolve(false) {
				t.Fatal("approval was not resolved exactly once")
			}
		}
	}
	if runner.count() != 1 {
		t.Fatalf("tool calls=%d", runner.count())
	}
	gotEvents := strings.Join(eventStrings(events), ",")
	for _, want := range []string{string(EventToolRunning), string(EventToolApproval), string(EventToolCompleted)} {
		if !strings.Contains(gotEvents, want) {
			t.Fatalf("events %s missing %s", gotEvents, want)
		}
	}
	client.mu.Lock()
	second := client.messages[1]
	client.mu.Unlock()
	if !strings.Contains(second[len(second)-1].Content, `"output":"tool output"`) {
		t.Fatalf("tool result not returned to King: %+v", second)
	}
}

func TestDeniedToolResultReturnsToKingWithoutFailingRun(t *testing.T) {
	client := &messageRecordingClient{responses: []string{
		`{"type":"tool","tool":{"id":"call-1","name":"write_file","arguments":{"path":"x","content":"x"}}}`,
		`{"type":"final","content":"understood"}`,
	}}
	var final *Result
	for event := range NewEngine(cfg(), client, WithTools(&recordingToolRunner{})).Stream(context.Background(), "p") {
		if event.Type == EventToolApproval {
			event.Approval.Resolve(false)
		}
		if event.Type == EventCompleted {
			final = event.Result
		}
	}
	if final == nil || final.Content != "understood" {
		t.Fatalf("final=%+v", final)
	}
	client.mu.Lock()
	second := client.messages[1]
	client.mu.Unlock()
	if !strings.Contains(second[len(second)-1].Content, `"denied":true`) {
		t.Fatalf("denial not returned to King: %+v", second)
	}
}

func TestDuplicateToolIDExecutesOnlyOnce(t *testing.T) {
	call := `{"type":"tool","tool":{"id":"same","name":"read_file","arguments":{"path":"x"}}}`
	client := &messageRecordingClient{responses: []string{call, call, `{"type":"final","content":"done"}`}}
	runner := &recordingToolRunner{}
	for range NewEngine(cfg(), client, WithTools(runner)).Stream(context.Background(), "p") {
	}
	if runner.count() != 1 {
		t.Fatalf("duplicate executed: %d", runner.count())
	}
}

func TestCancellationWhileApprovalPendingClosesStream(t *testing.T) {
	client := &messageRecordingClient{responses: []string{`{"type":"tool","tool":{"id":"call-1","name":"write_file","arguments":{"path":"x","content":"x"}}}`}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := NewEngine(cfg(), client, WithTools(&recordingToolRunner{})).Stream(ctx, "p")
	for event := range stream {
		if event.Type == EventToolApproval {
			cancel()
		}
	}
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("stream remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("stream blocked after cancellation")
	}
}

func TestToolActionRequiresConfiguredRunner(t *testing.T) {
	if _, err := parseAction(`{"type":"tool","tool":{"id":"1","name":"read_file","arguments":{"path":"x"}}}`); err != nil {
		t.Fatal(err)
	}
	client := &messageRecordingClient{responses: []string{`{"type":"tool","tool":{"id":"1","name":"read_file","arguments":{"path":"x"}}}`, `{"type":"final","content":"fallback"}`}}
	for event := range NewEngine(cfg(), client).Stream(context.Background(), "p") {
		if event.Type == EventToolRunning || event.Type == EventToolApproval {
			t.Fatalf("tool ran without configured runner: %+v", event)
		}
	}
}

func TestToolIntegrationWritesApprovedFile(t *testing.T) {
	workspace := t.TempDir()
	runner, err := tools.NewRunner(workspace)
	if err != nil {
		t.Fatal(err)
	}
	client := &messageRecordingClient{responses: []string{
		`{"type":"tool","tool":{"id":"write-1","name":"write_file","arguments":{"path":"notes.txt","content":"hello"}}}`,
		`{"type":"final","content":"saved"}`,
	}}
	for event := range NewEngine(cfg(), client, WithTools(runner)).Stream(context.Background(), "save a note") {
		if event.Type == EventToolApproval {
			event.Approval.Resolve(true)
		}
	}
	content, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	info, err := os.Stat(filepath.Join(workspace, "notes.txt"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestToolOutputIsBoundedBeforeReturningToKing(t *testing.T) {
	client := &messageRecordingClient{responses: []string{
		`{"type":"tool","tool":{"id":"read-1","name":"read_file","arguments":{"path":"large.txt"}}}`,
		`{"type":"final","content":"done"}`,
	}}
	runner := &recordingToolRunner{output: strings.Repeat("x", 30*1024)}
	var completed *tools.Result
	for event := range NewEngine(cfg(), client, WithTools(runner)).Stream(context.Background(), "read") {
		if event.Type == EventToolCompleted {
			completed = event.ToolResult
		}
	}
	if completed == nil || !completed.Truncated || len(completed.Output) > 24*1024 {
		t.Fatalf("unbounded tool result: %+v", completed)
	}
}

func eventStrings(events []EventType) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = string(event)
	}
	return out
}
