package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/topology"
	"io"
	"strings"
	"sync"
)

type ChatClient = modelapi.ChatClient
type EventType string

const (
	EventStarted          EventType = "started"
	EventKingThinking     EventType = "king-thinking"
	EventWorkersRunning   EventType = "workers-running"
	EventCouncilReviewing EventType = "council-reviewing"
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
	Type    EventType
	Message string
	Result  *Result
	TaskID  string
	Content string
}
type Engine struct {
	cfg    config.Config
	client ChatClient
}

func NewEngine(cfg config.Config, c ChatClient) *Engine {
	cfg.Topology.Endpoints = append([]topology.Endpoint(nil), cfg.Topology.Endpoints...)
	return &Engine{cfg: cfg, client: c}
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
		var workers []taskResult
		var reviews []string
		kingCalls := 0
		malformedBudget := false
		for rounds := 0; rounds < 4 && kingCalls < 4; rounds++ {
			if ctx.Err() != nil {
				return
			}
			msgs := []modelapi.Message{{Role: "system", Content: "You are the King. Respond with JSON action."}, {Role: "user", Content: prompt}}
			if len(workers) > 0 {
				msgs = append(msgs, modelapi.Message{Role: "user", Content: formatOutcomes(workers, reviews)})
			}
			kingCalls++
			raw, err = e.client.Chat(ctx, kingEp, kingAs.Model, msgs)
			if err != nil {
				fail(err)
				return
			}
			act, perr := parseAction(raw)
			if perr != nil {
				if kingCalls >= 4 {
					malformedBudget = true
				}
				if kingCalls >= 4 {
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
					emit(Event{Type: EventCompleted, Result: &Result{Content: raw, FallbackRaw: true, LimitReached: kingCalls >= 4, Message: func() string {
						if kingCalls >= 4 {
							return "king call limit reached"
						}
						return ""
					}()}})
					return
				}
			}
			if act.Type == "delegate" && kingCalls >= 4 {
				emit(Event{Type: EventCompleted, Result: &Result{Content: raw, LimitReached: true, Message: "king call limit reached"}})
				return
			}
			if act.Type == "final" {
				emit(Event{Type: EventCompleted, Result: &Result{Content: act.Content}})
				return
			}
			emit(Event{Type: EventWorkersRunning})
			workers = e.runWorkers(ctx, act.Tasks)
			if ctx.Err() != nil {
				return
			}
			emit(Event{Type: EventCouncilReviewing})
			reviews = e.runCouncil(ctx, prompt, workers)
			if ctx.Err() != nil {
				return
			}
			emit(Event{Type: EventKingThinking})
		}
		emit(Event{Type: EventCompleted, Result: &Result{Content: raw, LimitReached: true, FallbackRaw: malformedBudget, Message: "king call limit reached"}})
	}()
	return out
}

type action struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Tasks   []task `json:"tasks"`
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
		if strings.TrimSpace(a.Content) == "" {
			return a, fmt.Errorf("empty final")
		}
		return a, nil
	}
	if a.Type != "delegate" || len(a.Tasks) == 0 || len(a.Tasks) > 32 {
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
