package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
)

type ChatClient = modelapi.ChatClient
type EventType string

const (
	EventStarted          EventType = "started"
	EventKingThinking     EventType = "king-thinking"
	EventWorkersRunning   EventType = "workers-running"
	EventCouncilReviewing EventType = "council-reviewing"
	EventToolRunning      EventType = "tool-running"
	EventToolApproval     EventType = "tool-approval"
	EventToolCompleted    EventType = "tool-completed"
	EventCompleted        EventType = "completed"
	EventFailed           EventType = "failed"
)

type Result struct {
	Content      string `json:"content"`
	LimitReached bool   `json:"limit_reached,omitempty"`
	FallbackRaw  bool   `json:"fallback_raw,omitempty"`
	Error        string `json:"error,omitempty"`
	Message      string `json:"message,omitempty"`
}
type Event struct {
	Type       EventType
	Message    string
	Result     *Result
	TaskID     string
	Content    string
	Approval   *ApprovalRequest
	ToolCall   *tools.Call
	ToolResult *tools.Result
}

type ToolRunner interface {
	Run(context.Context, tools.Call, tools.Approver) tools.Result
}

type Engine struct {
	cfg    config.Config
	client ChatClient
	tools  ToolRunner
}

func NewEngine(cfg config.Config, c ChatClient) *Engine {
	cfg.Topology.Endpoints = append([]topology.Endpoint(nil), cfg.Topology.Endpoints...)
	return &Engine{cfg: cfg, client: c}
}

func NewEngineWithTools(cfg config.Config, client ChatClient, runner ToolRunner) *Engine {
	engine := NewEngine(cfg, client)
	engine.tools = runner
	return engine
}
func (e *Engine) Stream(ctx context.Context, prompt string) <-chan Event {
	out := make(chan Event, 128)
	go func() {
		defer close(out)
		emit := func(ev Event) {
			select {
			case out <- ev:
			case <-ctx.Done():
			}
		}
		emit(Event{Type: EventStarted})
		fail := func(err error) {
			emit(Event{Type: EventFailed, Message: err.Error(), Result: &Result{Error: err.Error(), Message: err.Error()}})
		}
		if strings.TrimSpace(prompt) == "" {
			fail(fmt.Errorf("prompt required"))
			return
		}
		if err := e.cfg.Validate(); err != nil {
			fail(err)
			return
		}
		if e.client == nil {
			fail(fmt.Errorf("missing chat client"))
			return
		}
		kingEp, kingAs, err := e.endpoint(e.cfg.Topology.Roles.King)
		if err != nil {
			fail(err)
			return
		}
		if _, _, err = e.endpoint(e.cfg.Topology.Roles.Worker); err != nil {
			fail(err)
			return
		}
		if ca := e.cfg.Topology.EffectiveCouncil(); ca != nil {
			if _, _, err = e.endpoint(*ca); err != nil {
				fail(err)
				return
			}
		}
		emit(Event{Type: EventKingThinking})
		raw := ""
		var feedback []string
		kingCalls := 0
		malformedBudget := false
		kingLimit := 4
		if e.tools != nil {
			kingLimit = 8
		}
		seenToolCalls := make(map[string]struct{})
		for kingCalls < kingLimit {
			if ctx.Err() != nil {
				return
			}
			msgs := []modelapi.Message{{Role: "system", Content: e.kingSystemPrompt()}, {Role: "user", Content: prompt}}
			for _, item := range feedback {
				msgs = append(msgs, modelapi.Message{Role: "user", Content: item})
			}
			kingCalls++
			raw, err = e.client.Chat(ctx, kingEp, kingAs.Model, msgs)
			if err != nil {
				fail(err)
				return
			}
			act, perr := parseAction(raw)
			if perr != nil {
				if kingCalls >= kingLimit {
					malformedBudget = true
				}
				if kingCalls >= kingLimit {
					emit(Event{Type: EventCompleted, Result: &Result{Content: raw, LimitReached: true, FallbackRaw: true, Message: "king call limit reached"}})
					return
				}
				kingCalls++
				repair, re := e.client.Chat(ctx, kingEp, kingAs.Model, []modelapi.Message{{Role: "system", Content: "Repair malformed action; output exact JSON."}, {Role: "user", Content: raw}})
				if re != nil {
					fail(re)
					return
				}
				act, perr = parseAction(repair)
				if perr != nil {
					raw = repair
				}
				if perr != nil {
					emit(Event{Type: EventCompleted, Result: &Result{Content: raw, FallbackRaw: true, LimitReached: kingCalls >= kingLimit, Message: func() string {
						if kingCalls >= kingLimit {
							return "king call limit reached"
						}
						return ""
					}()}})
					return
				}
			}
			if act.Type != "final" && kingCalls >= kingLimit {
				emit(Event{Type: EventCompleted, Result: &Result{Content: raw, LimitReached: true, Message: "king call limit reached"}})
				return
			}
			if act.Type == "final" {
				emit(Event{Type: EventCompleted, Result: &Result{Content: act.Content}})
				return
			}
			if act.Type == "delegate" {
				emit(Event{Type: EventWorkersRunning})
				workers := e.runWorkers(ctx, act.Tasks)
				if ctx.Err() != nil {
					return
				}
				emit(Event{Type: EventCouncilReviewing})
				reviews := e.runCouncil(ctx, prompt, workers)
				if ctx.Err() != nil {
					return
				}
				feedback = append(feedback, formatOutcomes(workers, reviews))
				emit(Event{Type: EventKingThinking})
				continue
			}

			toolResult := tools.Result{ID: act.Tool.ID, Name: act.Tool.Name}
			if e.tools == nil {
				toolResult.Error = "tools unavailable"
			} else if _, duplicate := seenToolCalls[act.Tool.ID]; duplicate {
				toolResult.Error = "duplicate tool call id"
			} else {
				seenToolCalls[act.Tool.ID] = struct{}{}
				call := *act.Tool
				emit(Event{Type: EventToolRunning, ToolCall: &call})
				approver := tools.ApproverFunc(func(ctx context.Context, approval tools.Approval) (bool, error) {
					request := NewApprovalRequest(approval)
					emit(Event{Type: EventToolApproval, Approval: request, ToolCall: &call})
					return request.Wait(ctx)
				})
				toolResult = e.tools.Run(ctx, call, approver)
				if ctx.Err() != nil {
					return
				}
				toolResult = boundToolResult(toolResult)
				emit(Event{Type: EventToolCompleted, ToolCall: &call, ToolResult: &toolResult})
			}
			encoded, marshalErr := json.Marshal(toolResult)
			if marshalErr != nil {
				fail(marshalErr)
				return
			}
			feedback = append(feedback, "Tool result: "+string(encoded))
			emit(Event{Type: EventKingThinking})
		}
		emit(Event{Type: EventCompleted, Result: &Result{Content: raw, LimitReached: true, FallbackRaw: malformedBudget, Message: "king call limit reached"}})
	}()
	return out
}

func boundToolResult(result tools.Result) tools.Result {
	const maxOutput = 24 * 1024
	if len(result.Output) > maxOutput {
		result.Output = truncateUTF8(result.Output, maxOutput)
		result.Truncated = true
	}
	if len(result.Error) > maxOutput {
		result.Error = truncateUTF8(result.Error, maxOutput)
		result.Truncated = true
	}
	return result
}

func truncateUTF8(value string, limit int) string {
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (e *Engine) kingSystemPrompt() string {
	base := "You are the King. Respond with JSON action."
	if e.tools == nil {
		return base
	}
	return base + ` Use exactly one action per response:
{"type":"final","content":"..."}
{"type":"delegate","tasks":[{"id":"...","prompt":"..."}]}
{"type":"tool","tool":{"id":"unique-id","name":"tool-name","arguments":{...}}}
Only you may request tools. Available tools: list_files(path,max_depth), read_file(path), search(path,query), write_file(path,content), edit_file(path,old,new), run_command(command). Read tools run automatically. Writes, edits, and commands require the user to approve every call.`
}

type action struct {
	Type    string      `json:"type"`
	Content string      `json:"content,omitempty"`
	Tasks   []task      `json:"tasks,omitempty"`
	Tool    *tools.Call `json:"tool,omitempty"`
}
type task struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}
type taskResult struct {
	ID, Prompt, Content string
	Err                 error
}

func parseAction(s string) (action, error) {
	var a action
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return a, fmt.Errorf("malformed action")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return a, fmt.Errorf("malformed action")
	}
	if a.Type == "final" {
		if strings.TrimSpace(a.Content) == "" || len(a.Tasks) != 0 || a.Tool != nil {
			return a, fmt.Errorf("empty final")
		}
		return a, nil
	}
	if a.Type == "tool" {
		if a.Tool == nil || strings.TrimSpace(a.Tool.ID) == "" || strings.TrimSpace(a.Tool.Name) == "" || len(a.Tool.Arguments) == 0 || strings.TrimSpace(a.Content) != "" || len(a.Tasks) != 0 {
			return a, fmt.Errorf("invalid tool action")
		}
		var arguments any
		if err := json.Unmarshal(a.Tool.Arguments, &arguments); err != nil {
			return a, fmt.Errorf("invalid tool arguments")
		}
		if _, ok := arguments.(map[string]any); !ok {
			return a, fmt.Errorf("invalid tool arguments")
		}
		return a, nil
	}
	if a.Type != "delegate" || strings.TrimSpace(a.Content) != "" || a.Tool != nil || len(a.Tasks) == 0 || len(a.Tasks) > 32 {
		return a, fmt.Errorf("invalid delegation")
	}
	seen := map[string]bool{}
	for _, t := range a.Tasks {
		if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Prompt) == "" || seen[t.ID] {
			return a, fmt.Errorf("invalid task")
		}
		seen[t.ID] = true
	}
	return a, nil
}
func (e *Engine) endpoint(a topology.Assignment) (topology.Endpoint, topology.Assignment, error) {
	if !a.Complete() {
		return topology.Endpoint{}, a, fmt.Errorf("missing assignment")
	}
	for _, ep := range e.cfg.Topology.Endpoints {
		if ep.ID == a.EndpointID {
			return ep, a, nil
		}
	}
	return topology.Endpoint{}, a, fmt.Errorf("missing endpoint %s", a.EndpointID)
}
func (e *Engine) runWorkers(ctx context.Context, tasks []task) []taskResult {
	res := make([]taskResult, len(tasks))
	concurrency := e.cfg.WorkerConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	ep, a, err := e.endpoint(e.cfg.Topology.Roles.Worker)
	for i, t := range tasks {
		res[i] = taskResult{ID: t.ID, Prompt: t.Prompt}
		if err != nil {
			res[i].Err = err
			continue
		}
		wg.Add(1)
		go func(i int, t task) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				res[i].Err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			s, er := e.client.Chat(ctx, ep, a.Model, []modelapi.Message{{Role: "system", Content: "You are a Worker. Solve the assigned task."}, {Role: "user", Content: t.Prompt}})
			res[i].Content, res[i].Err = s, er
		}(i, t)
	}
	wg.Wait()
	return res
}
func (e *Engine) runCouncil(ctx context.Context, prompt string, w []taskResult) []string {
	a := e.cfg.Topology.EffectiveCouncil()
	if a == nil {
		return nil
	}
	ep, as, err := e.endpoint(*a)
	if err != nil {
		return []string{err.Error()}
	}
	n := e.cfg.CouncilSize
	if n < 1 {
		n = 1
	}
	r := make([]string, n)
	var wg sync.WaitGroup
	for i := range r {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmsg := fmt.Sprintf("Review slot %d of %d.\nOriginal prompt: %s\nOutcomes:\n%s", i+1, len(r), prompt, formatOutcomes(w, nil))
			s, er := e.client.Chat(ctx, ep, as.Model, []modelapi.Message{{Role: "system", Content: "You are a Council reviewer. Review worker outcomes."}, {Role: "user", Content: cmsg}})
			if er != nil {
				r[i] = er.Error()
			} else {
				r[i] = s
			}
		}(i)
	}
	wg.Wait()
	return r
}
func formatOutcomes(w []taskResult, r []string) string {
	var b strings.Builder
	for _, x := range w {
		fmt.Fprintf(&b, "task %s: %s\n", x.ID, x.Content)
		if x.Err != nil {
			fmt.Fprintf(&b, " error: %v\n", x.Err)
		}
	}
	for i, x := range r {
		fmt.Fprintf(&b, "review %d: %s\n", i+1, x)
	}
	return b.String()
}
