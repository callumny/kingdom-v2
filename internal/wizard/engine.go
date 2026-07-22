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
const maxWizardCorrections = 2

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
	corrections := 0
	completedTools := make(map[string]bool)
	completedOrder := make([]string, 0, 4)
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
				reply := e.fallbackReply()
				e.history = append(e.history, modelapi.Message{Role: "assistant", Content: reply.Content})
				return reply, nil
			}
			e.history = append(e.history, modelapi.Message{Role: "user", Content: "Return one valid JSON action only. Do not use markdown or extra text."})
			continue
		}
		e.history = append(e.history, modelapi.Message{Role: "assistant", Content: raw})
		switch action.Type {
		case "message":
			missing := missingExplicitTools(userMessage, completedTools)
			if len(missing) > 0 {
				if corrections >= maxWizardCorrections {
					return Reply{}, fmt.Errorf("Wizard did not complete requested setting(s): %s", strings.Join(missing, ", "))
				}
				corrections++
				e.history = append(e.history, modelapi.Message{Role: "user", Content: "That summary is premature. The original request explicitly requires these successful tools: " + strings.Join(missing, ", ") + ". Continue with those changes before summarizing."})
				continue
			}
			if len(completedOrder) > 0 {
				return Reply{Content: e.session.ChangeSummary(completedOrder), Ready: e.session.Ready()}, nil
			}
			return Reply{Content: action.Content, Ready: action.Ready}, nil
		case "tool":
			toolCalls++
			result := e.session.Run(ctx, tools.Call{ID: fmt.Sprintf("wizard-%d", toolCalls), Name: action.Name, Arguments: action.Arguments})
			if result.Error == "" {
				if !completedTools[action.Name] {
					completedOrder = append(completedOrder, action.Name)
				}
				completedTools[action.Name] = true
			}
			encoded, _ := json.Marshal(result)
			e.history = append(e.history, modelapi.Message{Role: "user", Content: "Wizard tool result: " + string(encoded) +
				"\nOriginal request: " + strings.TrimSpace(userMessage) +
				"\nContinue with every requested change that has not succeeded yet. Only summarize after every part of the original request is complete."})
		}
	}
	return Reply{}, errors.New("Wizard tool limit reached")
}

func missingExplicitTools(request string, completed map[string]bool) []string {
	normalized := strings.ToLower(strings.TrimSpace(request))
	if hasAnyPrefix(normalized, "should ", "why ", "how ", "what ", "when ", "do i ", "does ", "is ", "are ") || containsAny(normalized, "explain", "recommend", "advise") {
		return nil
	}
	required := make([]string, 0, 5)
	if strings.Contains(normalized, "council") && containsAny(normalized, "enable", "disable", "turn on", "turn off", "without", "remove") {
		required = append(required, "enable_council")
	}
	if strings.Contains(normalized, "council") && containsAny(normalized, "member", "reviewer", "council size", "how many") {
		required = append(required, "set_council_size")
	}
	if strings.Contains(normalized, "worker concurrency") || strings.Contains(normalized, "concurrent worker") || strings.Contains(normalized, "workers") {
		required = append(required, "set_worker_concurrency")
	}
	if strings.Contains(normalized, "ollama") && containsAny(normalized, "shared", "separate") {
		required = append(required, "set_ollama_server_mode")
	}
	if strings.Contains(normalized, "port") && containsAny(normalized, "ollama", "mlx") {
		required = append(required, "set_provider_port")
	}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if !completed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
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
The draft already contains sensible defaults. Prefer explaining them in one short response. If the user asks for changes, make a private checklist of every requested change. Call one small tool at a time, inspect each result, and complete the whole checklist before summarizing.
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

func (e *Engine) fallbackReply() Reply {
	return Reply{
		Content:  "I couldn't reliably interpret the local model's response, so I stopped rather than claim a change. The Proposed Kingdom below is the source of truth; retry or use Tab for Manual setup.",
		Ready:    e.session.Ready(),
		Fallback: true,
	}
}
