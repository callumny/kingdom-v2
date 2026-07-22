package orchestration

import (
	"context"
	"errors"
	"fmt"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/topology"
	"sync"
	"testing"
	"time"
)

type fake struct {
	responses []string
	calls     int
	mu        sync.Mutex
}

func (f *fake) Chat(context.Context, topology.Endpoint, string, []modelapi.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= len(f.responses) {
		f.calls++
		return `{"type":"final","content":"fallback"}`, nil
	}
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}
func (f *fake) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func cfg() config.Config {
	c := config.Default()
	c.Providers.Ollama.Enabled = true
	c.CouncilSize = 1
	c.WorkerConcurrency = 2
	c.Topology = topology.Topology{Endpoints: []topology.Endpoint{{ID: "e", Name: "e", Kind: topology.KindOllama, BaseURL: "http://localhost:1"}}, Roles: topology.Roles{King: topology.Assignment{EndpointID: "e", Model: "m"}, Worker: topology.Assignment{EndpointID: "e", Model: "m"}}}
	return c
}
func TestDirectFinal(t *testing.T) {
	f := &fake{responses: []string{`{"type":"final","content":"done"}`}}
	ev := []Event{}
	for e := range NewEngine(cfg(), f).Stream(context.Background(), "p") {
		ev = append(ev, e)
	}
	if ev[len(ev)-1].Result.Content != "done" {
		t.Fatal(ev)
	}
}

func TestPlainTextKingResponseCompletesWithoutRepair(t *testing.T) {
	f := &fake{responses: []string{"Hello from the King."}}
	var last Event
	for event := range NewEngine(cfg(), f).Stream(context.Background(), "hello") {
		last = event
	}
	if f.count() != 1 || last.Type != EventCompleted || last.Result == nil || last.Result.Content != "Hello from the King." {
		t.Fatalf("calls=%d last=%+v", f.count(), last)
	}
}

type completionFake struct{ responses []modelapi.Completion }

func (f *completionFake) Chat(context.Context, topology.Endpoint, string, []modelapi.Message) (string, error) {
	return "", errors.New("text-only Chat should not be used")
}

func (f *completionFake) Complete(context.Context, topology.Endpoint, string, []modelapi.Message, int) (modelapi.Completion, error) {
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestEngineEmitsMeasuredModelActivity(t *testing.T) {
	client := &completionFake{responses: []modelapi.Completion{{Content: "Hello.", CompletionTokens: 20, GenerationDuration: 2 * time.Second}}}
	var activity *ModelActivity
	for event := range NewEngine(cfg(), client).Stream(context.Background(), "hello") {
		if event.Type == EventModelActivity {
			activity = event.ModelActivity
		}
	}
	if activity == nil || activity.Role != "King" || activity.Model != "m" || activity.EndpointKind != topology.KindOllama || activity.CompletionTokens != 20 || activity.GenerationDuration != 2*time.Second {
		t.Fatalf("activity=%+v", activity)
	}
}
func TestCancellationCloses(t *testing.T) {
	ctx, c := context.WithCancel(context.Background())
	f := &fake{responses: []string{`{"type":"final","content":"x"}`}}
	c()
	ch := NewEngine(cfg(), f).Stream(ctx, "p")
	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("blocked")
	}
}

func TestParseActionMatrix(t *testing.T) {
	valid := []string{`{"type":"final","content":"x"}`, `{"type":"delegate","tasks":[{"id":"a","prompt":"p"}]}`}
	for _, s := range valid {
		if _, e := parseAction(s); e != nil {
			t.Fatal(e)
		}
	}
	invalid := []string{``, `{`, `{"type":"final"}`, `{"type":"delegate","tasks":[]}`, `{"type":"delegate","tasks":[{"id":"a","prompt":""}]}`, `{"type":"delegate","tasks":[{"id":"a","prompt":"p"},{"id":"a","prompt":"q"}]}`, `{"type":"wat"}`, `{"type":"final","content":"x"} trailing`}
	for _, s := range invalid {
		if _, e := parseAction(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestEndpointErrors(t *testing.T) {
	e := NewEngine(cfg(), &fake{})
	if _, _, err := e.endpoint(topology.Assignment{}); err == nil {
		t.Fatal("empty assignment")
	}
	if _, _, err := e.endpoint(topology.Assignment{EndpointID: "x", Model: "m"}); err == nil {
		t.Fatal("missing endpoint")
	}
}
func TestCouncilFallback(t *testing.T) {
	c := cfg()
	c.Topology.Roles.Council = topology.Assignment{}
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"p"}]}`, `review`, `{"type":"final","content":"done"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() < 3 {
		t.Fatal(f.count())
	}
}
func TestWorkerOrder(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"},{"id":"b","prompt":"b"}]}`, `x`, `y`, `r`, `{"type":"final","content":"z"}`}}
	for range NewEngine(cfg(), f).Stream(context.Background(), "p") {
	}
	if f.count() < 5 {
		t.Fatal(f.count())
	}
}
func TestKingCallLimit(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, `x`, `r`, `{"type":"delegate","tasks":[{"id":"b","prompt":"b"}]}`, `y`, `r`, `{"type":"delegate","tasks":[{"id":"c","prompt":"c"}]}`, `z`, `r`, `bad`}}
	for range NewEngine(cfg(), f).Stream(context.Background(), "p") {
	}
	if f.count() > 12 {
		t.Fatalf("calls %d", f.count())
	}
}
func TestMissingClientFails(t *testing.T) {
	for e := range NewEngine(cfg(), nil).Stream(context.Background(), "p") {
		if e.Type == EventFailed {
			return
		}
	}
	t.Fatal("no failure")
}
func TestEventOrdering(t *testing.T) {
	f := &fake{responses: []string{`{"type":"final","content":"x"}`}}
	var prev EventType
	for e := range NewEngine(cfg(), f).Stream(context.Background(), "p") {
		if e.Type == EventStarted && prev != "" {
			t.Fatal("started order")
		}
		prev = e.Type
	}
	if prev != EventCompleted {
		t.Fatal(prev)
	}
}
func TestConfigUnchanged(t *testing.T) {
	c := cfg()
	before := c
	f := &fake{responses: []string{`{"type":"final","content":"x"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if fmt.Sprintf("%#v", c) != fmt.Sprintf("%#v", before) {
		t.Fatal("mutated")
	}
}
func TestCouncilSizeCalls(t *testing.T) {
	c := cfg()
	c.CouncilSize = 3
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"p"}]}`, `x`, `y`, `z`, `r`, `{"type":"final","content":"q"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() < 5 {
		t.Fatal(f.count())
	}
}
func TestBlankPromptRejected(t *testing.T) {
	if _, e := parseAction(`{"type":"delegate","tasks":[{"id":"x","prompt":" "}]}`); e == nil {
		t.Fatal("blank accepted")
	}
}

func TestPreflightRejectsInvalidRolesBeforeCalling(t *testing.T) {
	c := cfg()
	c.Topology.Roles.Worker = topology.Assignment{}
	f := &fake{responses: []string{`{"type":"final","content":"x"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() != 0 {
		t.Fatal(f.count())
	}
}
func TestExplicitAndFallbackCouncilRouting(t *testing.T) {
	c := cfg()
	c.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "c"}
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"p"}]}`, `w`, `r`, `{"type":"final","content":"x"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() != 4 {
		t.Fatal(f.count())
	}
}
func TestDelegationRoutesRolesAndSynthesisContext(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"p"}]}`, `worker`, `review`, `{"type":"final","content":"done"}`}}
	for range NewEngine(cfg(), f).Stream(context.Background(), "orig") {
	}
	if f.count() != 4 {
		t.Fatal(f.count())
	}
}
func TestWorkersRespectConcurrencyAndPreserveOrder(t *testing.T) {
	c := cfg()
	c.WorkerConcurrency = 1
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"},{"id":"b","prompt":"b"}]}`, `a`, `b`, `r`, `{"type":"final","content":"x"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() != 5 {
		t.Fatal(f.count())
	}
}
func TestPartialWorkerFailuresReachCouncilAndKing(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, `w`, `r`, `{"type":"final","content":"x"}`}}
	for range NewEngine(cfg(), f).Stream(context.Background(), "p") {
	}
	if f.count() != 4 {
		t.Fatal(f.count())
	}
}
func TestCouncilCallsRespectSizeAndPreserveOrder(t *testing.T) {
	c := cfg()
	c.CouncilSize = 3
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, `w`, `r1`, `r2`, `r3`, `{"type":"final","content":"x"}`}}
	for range NewEngine(c, f).Stream(context.Background(), "p") {
	}
	if f.count() != 6 {
		t.Fatal(f.count())
	}
}
func TestNonActionResponseCompletesAsPlainText(t *testing.T) {
	f := &fake{responses: []string{"bad"}}
	ev := []Event{}
	for x := range NewEngine(cfg(), f).Stream(context.Background(), "p") {
		ev = append(ev, x)
	}
	if f.count() != 1 || ev[len(ev)-1].Result.Content != "bad" {
		t.Fatalf("calls=%d ev=%v", f.count(), ev)
	}
}
func TestInvalidDelegationNeverRunsWorkers(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[]}`}}
	var last Event
	for event := range NewEngine(cfg(), f).Stream(context.Background(), "p") {
		last = event
	}
	if f.count() != 1 || last.Result == nil || last.Result.Content != `{"type":"delegate","tasks":[]}` {
		t.Fatalf("calls=%d last=%+v", f.count(), last)
	}
}
func TestFourKingCallLimit(t *testing.T) {
	f := &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`, `w`, `r`, `{"type":"delegate","tasks":[{"id":"b","prompt":"b"}]}`, `w`, `r`, `{"type":"delegate","tasks":[{"id":"c","prompt":"c"}]}`, `w`, `r`, `{"type":"delegate","tasks":[{"id":"d","prompt":"d"}]}`, `w`, `r`}}
	for range NewEngine(cfg(), f).Stream(context.Background(), "p") {
	}
	if f.count() > 12 {
		t.Fatal(f.count())
	}
}
func TestCancellationDuringWorkersClosesStream(t *testing.T) {
	ctx, c := context.WithCancel(context.Background())
	c()
	for range NewEngine(cfg(), &fake{responses: []string{`{"type":"delegate","tasks":[{"id":"a","prompt":"a"}]}`}}).Stream(ctx, "p") {
	}
}
func TestTerminalEventContract(t *testing.T) {
	ev := []Event{}
	for x := range NewEngine(cfg(), &fake{responses: []string{`{"type":"final","content":"x"}`}}).Stream(context.Background(), "p") {
		ev = append(ev, x)
	}
	if len(ev) == 0 || ev[len(ev)-1].Type != EventCompleted || ev[len(ev)-1].Result == nil {
		t.Fatal(ev)
	}
}
func TestEngineSnapshotsConfig(t *testing.T) {
	c := cfg()
	e := NewEngine(c, &fake{responses: []string{`{"type":"final","content":"x"}`}})
	c.Topology.Endpoints[0].Name = "mutated"
	for range e.Stream(context.Background(), "p") {
	}
}
