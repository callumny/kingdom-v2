package app

import (
	"context"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/tools"
)

func TestApprovalKeysResolvePendingRequest(t *testing.T) {
	for _, tc := range []struct {
		key      string
		approved bool
	}{
		{key: "y", approved: true},
		{key: "n", approved: false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			request := orchestration.NewApprovalRequest(tools.Approval{
				Call:    tools.Call{ID: "1", Name: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
				Summary: "notes.txt",
				Risk:    "write",
			})
			m := New(completeConfig())
			m.running = true
			m.runGen = 1
			m, _ = update(m, chatEventMsg{Generation: 1, Event: orchestration.Event{Type: orchestration.EventToolApproval, Approval: request}})
			if m.approval == nil || !strings.Contains(m.View().Content, "notes.txt") || !strings.Contains(m.View().Content, "write_file") {
				t.Fatalf("approval not rendered: %s", m.View().Content)
			}
			m, _ = update(m, key(tc.key))
			approved, err := request.Wait(context.Background())
			if err != nil || approved != tc.approved || m.approval != nil {
				t.Fatalf("approved=%v err=%v pending=%v", approved, err, m.approval)
			}
		})
	}
}

func TestApprovalBlocksUnrelatedInputAndEscapeCancelsRun(t *testing.T) {
	request := orchestration.NewApprovalRequest(tools.Approval{Call: tools.Call{ID: "1", Name: "run_command", Arguments: []byte(`{"command":"go test ./..."}`)}, Summary: "go test ./...", Risk: "shell"})
	cancelled := false
	m := New(completeConfig())
	m.running = true
	m.runGen = 1
	m.runCancel = func() { cancelled = true }
	m.approval = request
	m.chat.SetValue("original")
	m, _ = update(m, key("x"))
	if m.chat.Value() != "original" || m.approval == nil {
		t.Fatal("unrelated input changed state while approval pending")
	}
	m, _ = update(m, key("esc"))
	if !cancelled || m.running || m.approval != nil {
		t.Fatalf("escape did not cancel pending run: %+v", m)
	}
}

func TestToolRequestAndResultAreAddedToTranscript(t *testing.T) {
	request := orchestration.NewApprovalRequest(tools.Approval{Call: tools.Call{ID: "1", Name: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)}, Summary: "notes.txt", Risk: "write"})
	m := New(completeConfig())
	m.running = true
	m.runGen = 1
	m, _ = update(m, chatEventMsg{Generation: 1, Event: orchestration.Event{Type: orchestration.EventToolApproval, Approval: request}})
	m, _ = update(m, key("y"))
	m, _ = update(m, chatEventMsg{Generation: 1, Event: orchestration.Event{Type: orchestration.EventToolCompleted, ToolResult: &tools.Result{ID: "1", Name: "write_file", Output: "ok"}}})
	transcript := strings.Join(m.history, "\n")
	if !strings.Contains(transcript, "write_file") || !strings.Contains(transcript, "ok") {
		t.Fatalf("missing tool transcript: %s", transcript)
	}
}
