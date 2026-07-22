package wizard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/tools"
)

const maxWizardToolCalls = 10

type Reply struct {
	Content  string
	Ready    bool
	Fallback bool
}

type Engine struct {
	mu      sync.Mutex
	client  modelapi.ChatClient
	model   setup.ModelOption
	session *Session
	history []modelapi.Message
}

func NewEngine(client modelapi.ChatClient, model setup.ModelOption, session *Session) *Engine {
	return &Engine{client: client, model: model, session: session}
}

func (e *Engine) Start(ctx context.Context) (Reply, error) {
	if err := e.validate(); err != nil {
		return Reply{}, err
	}
	if err := e.session.PrepareDefaults(); err != nil {
		return Reply{}, err
	}
	return e.Respond(ctx, "Briefly explain the prepared setup. Make no changes unless the draft is incomplete.")
}

func (e *Engine) Respond(ctx context.Context, userMessage string) (Reply, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.validate(); err != nil {
		return Reply{}, err
	}
	if strings.TrimSpace(userMessage) == "" {
		return Reply{}, errors.New("Wizard message is required")
	}
	if len(e.history) == 0 {
		prompt, err := e.systemPrompt(ctx)
		if err != nil {
			return Reply{}, err
		}
		e.history = append(e.history, modelapi.Message{Role: "system", Content: prompt})
	}
	e.history = append(e.history, modelapi.Message{Role: "user", Content: strings.TrimSpace(userMessage)})
	malformed := 0
	for toolCalls := 0; toolCalls < maxWizardToolCalls; {
		if err := ctx.Err(); err != nil {
			return Reply{}, err
		}
		raw, err := e.client.Chat(ctx, e.model.Endpoint, e.model.Ref.ModelID, append([]modelapi.Message(nil), e.history...))
		if err != nil {
			return Reply{}, err
		}
		action, err := parseAction(raw)
		if err != nil {
			e.history = append(e.history, modelapi.Message{Role: "assistant", Content: raw})
			malformed++
			if malformed > 1 {
				reply := deterministicRecommendation()
				e.history = append(e.history, modelapi.Message{Role: "assistant", Content: reply.Content})
				return reply, nil
			}
			e.history = append(e.history, modelapi.Message{Role: "user", Content: "Return one valid JSON action only. Do not use markdown or extra text."})
			continue
		}
		e.history = append(e.history, modelapi.Message{Role: "assistant", Content: raw})
		switch action.Type {
		case "message":
			return Reply{Content: action.Content, Ready: action.Ready}, nil
		case "tool":
			toolCalls++
			result := e.session.Run(ctx, tools.Call{ID: fmt.Sprintf("wizard-%d", toolCalls), Name: action.Name, Arguments: action.Arguments})
			encoded, _ := json.Marshal(result)
			e.history = append(e.history, modelapi.Message{Role: "user", Content: "Wizard tool result: " + string(encoded)})
		}
	}
	return Reply{}, errors.New("Wizard tool limit reached")
}

func (e *Engine) validate() error {
	if e == nil || e.client == nil {
		return errors.New("Wizard model client is unavailable")
	}
	if e.session == nil {
		return errors.New("Wizard setup session is unavailable")
	}
	if !e.session.HasModel(e.model.Ref) {
		return errors.New("Wizard model must be one of the selected models")
	}
	return nil
}

func (e *Engine) systemPrompt(ctx context.Context) (string, error) {
	inspection := e.session.Run(ctx, tools.Call{ID: "wizard-inspect", Name: "inspect_setup", Arguments: json.RawMessage(`{}`)})
	if inspection.Error != "" {
		return "", errors.New(inspection.Error)
	}
	definitions, err := json.Marshal(ToolDefinitions())
	if err != nil {
		return "", err
	}
	return `You are Kingdom's concise setup-only Wizard. Help the user configure only the selected local models.
The draft already contains sensible defaults. Prefer explaining them in one short response. If the user asks for a change, call one small tool at a time, inspect the result, then summarize.
You have no shell, files, memory, provider installation, or normal Kingdom tools. Never claim a setting changed unless its tool succeeded.
Return exactly one JSON object and no markdown:
{"type":"tool","name":"tool_name","arguments":{...}}
or {"type":"message","content":"one concise helpful response","ready":true}
Use ready=true when preview_setup is valid or when explaining the prepared valid draft.
Tools: ` + string(definitions) + `
Current setup: ` + inspection.Output, nil
}

type action struct {
	Type      string          `json:"type"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Content   string          `json:"content,omitempty"`
	Ready     bool            `json:"ready,omitempty"`
}

func parseAction(raw string) (action, error) {
	var parsed action
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return action{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return action{}, errors.New("trailing action data")
	}
	switch parsed.Type {
	case "tool":
		if strings.TrimSpace(parsed.Name) == "" || len(parsed.Arguments) == 0 || strings.TrimSpace(parsed.Content) != "" {
			return action{}, errors.New("invalid tool action")
		}
	case "message":
		if strings.TrimSpace(parsed.Content) == "" || parsed.Name != "" || len(parsed.Arguments) != 0 {
			return action{}, errors.New("invalid message action")
		}
	default:
		return action{}, errors.New("action type must be tool or message")
	}
	return parsed, nil
}

func deterministicRecommendation() Reply {
	return Reply{
		Content:  "I prepared a sensible setup using the larger selected model for King, the smaller model for Worker, and conservative performance defaults. You can apply it now or ask for one specific change.",
		Ready:    true,
		Fallback: true,
	}
}
