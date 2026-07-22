package orchestration

import (
	"context"
	"errors"
	"fmt"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/topology"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedCall struct {
	role, model, endpoint, user string
	msgs                        []modelapi.Message
}
type recordingClient struct {
	mu                sync.Mutex
	calls             []recordedCall
	active, maxActive int
	script            func(recordedCall) (string, error, time.Duration)
}

func (r *recordingClient) Chat(ctx context.Context, ep topology.Endpoint, model string, msgs []modelapi.Message) (string, error) {
	c := recordedCall{model: model, endpoint: ep.ID, msgs: append([]modelapi.Message(nil), msgs...)}
	if len(msgs) > 0 {
		c.role = msgs[0].Content
		for _, m := range msgs[1:] {
			c.user += m.Content
		}
	}
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.active--; r.mu.Unlock() }()
	if r.script == nil {
		return `{"type":"final","content":"ok"}`, nil
	}
	out, err, d := r.script(c)
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return out, err
}
func (r *recordingClient) snapshot() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCall(nil), r.calls...)
}

func isKingCall(call recordedCall) bool {
	return strings.HasPrefix(call.role, "You are the King.")
}

func TestBufferedStreamCompletesWithoutImmediateConsumer(t *testing.T) {
	f := &recordingClient{script: func(recordedCall) (string, error, time.Duration) { return `{"type":"final","content":"done"}`, nil, 0 }}
	ch := NewEngine(cfg(), f).Stream(context.Background(), "p")
	time.Sleep(20 * time.Millisecond)
	for range ch {
	}
}
func TestPlainTextDoesNotDispatch(t *testing.T) {
	workers, council := 0, 0
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		switch {
		case isKingCall(c):
			return "A direct answer.", nil, 0
		case c.role == "You are a Worker. Solve the assigned task.":
			workers++
			return "worker", nil, 0
		case c.role == "You are a Council reviewer. Review worker outcomes.":
			council++
			return "review", nil, 0
		}
		return `{"type":"final","content":"done"}`, nil, 0
	}}
	var last Event
	for e := range NewEngine(cfg(), f).Stream(context.Background(), "p") {
		last = e
	}
	if last.Result == nil || last.Result.Content != "A direct answer." || last.Result.LimitReached {
		t.Fatal(last)
	}
	if workers != 0 || council != 0 {
		t.Fatalf("workers=%d council=%d", workers, council)
	}
}
func TestKingTransportFailureEmitsFailed(t *testing.T) {
	sentinel := errors.New("King transport sentinel")
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		return "", sentinel, 0
	}}
	var events []Event
	for e := range NewEngine(cfg(), f).Stream(context.Background(), "prompt") {
		events = append(events, e)
	}
	if len(events) == 0 || events[len(events)-1].Type != EventFailed {
		t.Fatalf("events=%+v", events)
	}
	last := events[len(events)-1]
	if last.Result == nil || !strings.Contains(last.Result.Error, sentinel.Error()) || !strings.Contains(last.Result.Message, sentinel.Error()) {
		t.Fatalf("event=%+v", last)
	}
	for _, e := range events {
		if e.Type == EventCompleted {
			t.Fatalf("unexpected completed: %+v", events)
		}
	}
}
func TestRecordedRoleRoutingAndSynthesisContext(t *testing.T) {
	c := cfg()
	c.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "c"}
	i := 0
	vals := []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"task"}]}`, "worker", "review", `{"type":"final","content":"done"}`}
	f := &recordingClient{script: func(recordedCall) (string, error, time.Duration) { v := vals[i]; i++; return v, nil, 0 }}
	for range NewEngine(c, f).Stream(context.Background(), "orig") {
	}
	calls := f.snapshot()
	if len(calls) != 4 {
		t.Fatal(len(calls))
	}
	if !strings.Contains(calls[1].user, "task") || calls[1].role != "You are a Worker. Solve the assigned task." || calls[2].role != "You are a Council reviewer. Review worker outcomes." || !strings.Contains(calls[3].user, "worker") {
		t.Fatalf("calls=%+v", calls)
	}
}
func TestWorkerConcurrencyActuallyBoundedAndParallel(t *testing.T) {
	c := cfg()
	c.WorkerConcurrency = 2
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	kingCalls := 0
	var scriptMu sync.Mutex
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		if isKingCall(c) {
			scriptMu.Lock()
			defer scriptMu.Unlock()
			kingCalls++
			if kingCalls == 1 {
				return `{"type":"delegate","tasks":[{"id":"a","prompt":"a"},{"id":"b","prompt":"b"},{"id":"c","prompt":"c"}]}`, nil, 0
			}
			return `{"type":"final","content":"x"}`, nil, 0
		}
		if c.role == "You are a Worker. Solve the assigned task." {
			started <- struct{}{}
			<-release
			return c.user, nil, 0
		}
		return "review", nil, 0
	}}
	ch := NewEngine(c, f).Stream(context.Background(), "p")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("workers not started")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not parallel")
	}
	close(release)
	for range ch {
	}
	if f.maxActive > 2 {
		t.Fatalf("max=%d", f.maxActive)
	}
}
func TestReverseWorkerCompletionStillSynthesizesPlanOrder(t *testing.T) {
	c := cfg()
	c.WorkerConcurrency = 2
	kingCalls := 0
	var scriptMu sync.Mutex
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		if isKingCall(c) {
			scriptMu.Lock()
			defer scriptMu.Unlock()
			kingCalls++
			if kingCalls == 1 {
				return `{"type":"delegate","tasks":[{"id":"a","prompt":"a"},{"id":"b","prompt":"b"}]}`, nil, 0
			}
			return `{"type":"final","content":"x"}`, nil, 0
		}
		if c.role == "You are a Worker. Solve the assigned task." && c.user == "a" {
			return "result-a", nil, 40 * time.Millisecond
		}
		if c.role == "You are a Worker. Solve the assigned task." {
			return "result-b", nil, 0
		}
		return "review", nil, 0
	}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	for _, cc := range f.snapshot() {
		if isKingCall(cc) && strings.Contains(cc.user, "task a:") && strings.Index(cc.user, "task a:") > strings.Index(cc.user, "task b:") {
			t.Fatal(cc.user)
		}
	}
}
func TestPartialFailureTextReachesCouncilAndKing(t *testing.T) {
	c := cfg()
	c.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "c"}
	kingCalls := 0
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		switch {
		case isKingCall(c):
			kingCalls++
			if kingCalls == 1 {
				return `{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, nil, 0
			}
			return `{"type":"final","content":"x"}`, nil, 0
		case c.role == "You are a Worker. Solve the assigned task.":
			return "", errors.New("worker failed"), 0
		case c.role == "You are a Council reviewer. Review worker outcomes.":
			return "review", nil, 0
		}
		return "", errors.New("unexpected call"), 0
	}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	calls := f.snapshot()
	if len(calls) < 4 || !strings.Contains(calls[2].user, "worker failed") || !strings.Contains(calls[3].user, "worker failed") {
		t.Fatalf("calls=%+v", calls)
	}
}
func TestCouncilReviewsAreDeterministicallyNumbered(t *testing.T) {
	c := cfg()
	c.CouncilSize = 3
	c.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "c"}
	kingCalls := 0
	f := &recordingClient{script: func(call recordedCall) (string, error, time.Duration) {
		switch {
		case isKingCall(call):
			kingCalls++
			if kingCalls == 1 {
				return `{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, nil, 0
			}
			return `{"type":"final","content":"x"}`, nil, 0
		case call.role == "You are a Worker. Solve the assigned task.":
			return "w", nil, 0
		case call.role == "You are a Council reviewer. Review worker outcomes.":
			for i := 1; i <= 3; i++ {
				if strings.Contains(call.user, fmt.Sprintf("Review slot %d of 3", i)) {
					return fmt.Sprintf("r%d", i), nil, time.Duration(4-i) * time.Millisecond
				}
			}
		}
		return "", errors.New("unexpected call"), 0
	}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	calls := f.snapshot()
	found := false
	for _, c := range calls {
		if isKingCall(c) && strings.Contains(c.user, "review 1:") {
			found = true
			if !(strings.Index(c.user, "review 1: r1") < strings.Index(c.user, "review 2: r2") && strings.Index(c.user, "review 2: r2") < strings.Index(c.user, "review 3: r3")) {
				t.Fatal(c.user)
			}
		}
	}
	if !found {
		t.Fatal("missing synthesis call")
	}
}
func TestCancellationAfterWorkersStartClosesStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	f := &recordingClient{script: func(c recordedCall) (string, error, time.Duration) {
		if isKingCall(c) {
			return `{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, nil, 0
		}
		close(started)
		<-ctx.Done()
		return "", ctx.Err(), 0
	}}
	ch := NewEngine(cfg(), f).Stream(ctx, "p")
	<-started
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("stream blocked")
	}
}

var _ = fmt.Sprintf
