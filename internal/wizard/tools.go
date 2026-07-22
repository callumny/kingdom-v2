// Package wizard provides the bounded, setup-only agent used after model
// selection. Its tools mutate an in-memory setup draft and never access the
// filesystem, shell, provider installer, memory, or normal Kingdom tools.
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

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{Name: "inspect_setup", Description: "List selected models by number and show current settings.", Parameters: schema(`{"type":"object","additionalProperties":false}`)},
		{Name: "enable_council", Description: "Enable or disable the optional Council.", Parameters: schema(`{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"],"additionalProperties":false}`)},
		{Name: "assign_model", Description: "Assign one numbered selected model to one role.", Parameters: schema(`{"type":"object","properties":{"role":{"enum":["king","worker","council"]},"model_number":{"type":"integer","minimum":1,"maximum":3}},"required":["role","model_number"],"additionalProperties":false}`)},
		{Name: "set_council_size", Description: "Set the number of Council reviewers from 1 to 9.", Parameters: schema(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":9}},"required":["count"],"additionalProperties":false}`)},
		{Name: "set_worker_concurrency", Description: "Set concurrent Workers from 1 to 32.", Parameters: schema(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":32}},"required":["count"],"additionalProperties":false}`)},
		{Name: "set_ollama_server_mode", Description: "Use separate Ollama servers per model or one shared server.", Parameters: schema(`{"type":"object","properties":{"mode":{"enum":["separate","shared"]}},"required":["mode"],"additionalProperties":false}`)},
		{Name: "preview_setup", Description: "Validate and return the complete proposed setup.", Parameters: schema(`{"type":"object","additionalProperties":false}`)},
		{Name: "apply_setup", Description: "Validate and save setup after the user explicitly confirms Apply and launch.", Parameters: schema(`{"type":"object","additionalProperties":false}`)},
	}
}

func schema(value string) json.RawMessage { return json.RawMessage(value) }

type Session struct {
	mu              sync.Mutex
	draft           *setup.Draft
	save            func(config.Config) error
	applyAuthorized bool
}

func NewSession(draft *setup.Draft) *Session { return &Session{draft: draft} }

func NewSessionWithSave(draft *setup.Draft, save func(config.Config) error) *Session {
	return &Session{draft: draft, save: save}
}

// AuthorizeApply grants one attempt to call apply_setup. The authorization is
// consumed even when validation or saving fails, so it cannot be replayed.
func (s *Session) AuthorizeApply() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.applyAuthorized = true
	s.mu.Unlock()
}

func (s *Session) Run(ctx context.Context, call tools.Call) tools.Result {
	result := tools.Result{ID: call.ID, Name: call.Name}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result
	}
	if s == nil || s.draft == nil {
		result.Error = "setup draft is unavailable"
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var output any = map[string]bool{"ok": true}
	var err error
	switch call.Name {
	case "inspect_setup":
		err = decodeArguments(call.Arguments, &struct{}{})
		if err == nil {
			output = s.snapshot(false)
		}
	case "enable_council":
		var arguments struct {
			Enabled bool `json:"enabled"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			s.draft.SetCouncilEnabled(arguments.Enabled)
		}
	case "assign_model":
		var arguments struct {
			Role        string `json:"role"`
			ModelNumber int    `json:"model_number"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			err = s.assign(arguments.Role, arguments.ModelNumber)
		}
	case "set_council_size":
		var arguments struct {
			Count int `json:"count"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			if arguments.Count < 1 || arguments.Count > 9 {
				err = errors.New("council size must be 1..9")
			} else {
				s.draft.Config.CouncilSize = arguments.Count
			}
		}
	case "set_worker_concurrency":
		var arguments struct {
			Count int `json:"count"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			if arguments.Count < 1 || arguments.Count > 32 {
				err = errors.New("worker concurrency must be 1..32")
			} else {
				s.draft.Config.WorkerConcurrency = arguments.Count
			}
		}
	case "set_ollama_server_mode":
		var arguments struct {
			Mode string `json:"mode"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			err = s.setOllamaMode(arguments.Mode)
		}
	case "preview_setup":
		err = decodeArguments(call.Arguments, &struct{}{})
		if err == nil {
			output = s.snapshot(true)
		}
	case "apply_setup":
		err = decodeArguments(call.Arguments, &struct{}{})
		if err == nil {
			err = s.apply()
			if err == nil {
				output = map[string]bool{"applied": true}
			}
		}
	default:
		err = fmt.Errorf("unknown Wizard tool %q", call.Name)
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	encoded, encodeErr := json.Marshal(output)
	if encodeErr != nil {
		result.Error = encodeErr.Error()
		return result
	}
	result.Output = string(encoded)
	return result
}

func (s *Session) apply() error {
	if !s.applyAuthorized {
		return errors.New("Apply and launch confirmation is required")
	}
	s.applyAuthorized = false
	if err := validateDraft(s.draft); err != nil {
		return err
	}
	if s.save == nil {
		return errors.New("configuration saver is unavailable")
	}
	if err := s.save(s.draft.Config); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	return nil
}

func (s *Session) assign(role string, modelNumber int) error {
	selected := s.draft.SelectedModels()
	if modelNumber < 1 || modelNumber > len(selected) {
		return fmt.Errorf("model_number must be 1..%d", len(selected))
	}
	assignment := selected[modelNumber-1].Ref.Assignment()
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "king":
		s.draft.AssignKing(assignment)
	case "worker":
		s.draft.AssignWorker(assignment)
	case "council":
		s.draft.AssignCouncil(assignment)
	default:
		return errors.New("role must be king, worker, or council")
	}
	return nil
}

func (s *Session) setOllamaMode(mode string) error {
	hasOllama := false
	for _, option := range s.draft.SelectedModels() {
		if option.Ref.EndpointID == setup.OllamaEndpointID {
			hasOllama = true
			break
		}
	}
	if !hasOllama {
		return errors.New("no selected Ollama model uses this setting")
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "separate":
		s.draft.Config.Providers.Ollama.PortMode = config.OllamaDedicatedPorts
	case "shared":
		s.draft.Config.Providers.Ollama.PortMode = config.OllamaSharedPort
	default:
		return errors.New("mode must be separate or shared")
	}
	return nil
}

type modelSnapshot struct {
	Number        int    `json:"number"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ParameterSize string `json:"parameter_size,omitempty"`
}

type setupSnapshot struct {
	Ready             bool            `json:"ready"`
	Models            []modelSnapshot `json:"models"`
	King              int             `json:"king_model_number,omitempty"`
	Worker            int             `json:"worker_model_number,omitempty"`
	CouncilEnabled    bool            `json:"council_enabled"`
	Council           int             `json:"council_model_number,omitempty"`
	CouncilSize       int             `json:"council_size"`
	WorkerConcurrency int             `json:"worker_concurrency"`
	OllamaServerMode  string          `json:"ollama_server_mode,omitempty"`
	ValidationError   string          `json:"validation_error,omitempty"`
}

func (s *Session) snapshot(validate bool) setupSnapshot {
	selected := s.draft.SelectedModels()
	snapshot := setupSnapshot{
		CouncilEnabled:    s.draft.Config.CouncilEnabled,
		CouncilSize:       s.draft.Config.CouncilSize,
		WorkerConcurrency: s.draft.Config.WorkerConcurrency,
	}
	for index, option := range selected {
		provider := option.Endpoint.Name
		if provider == "" {
			provider = option.Ref.EndpointID
		}
		snapshot.Models = append(snapshot.Models, modelSnapshot{Number: index + 1, Provider: provider, Model: option.Ref.ModelID, ParameterSize: option.ParameterSize})
	}
	snapshot.King = assignmentNumber(s.draft.Config.Topology.Roles.King, selected)
	snapshot.Worker = assignmentNumber(s.draft.Config.Topology.Roles.Worker, selected)
	snapshot.Council = assignmentNumber(s.draft.Config.Topology.Roles.Council, selected)
	if hasSelectedOllama(selected) {
		snapshot.OllamaServerMode = "shared"
		if s.draft.Config.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
			snapshot.OllamaServerMode = "separate"
		}
	}
	if validate {
		if err := validateDraft(s.draft); err != nil {
			snapshot.ValidationError = err.Error()
		} else {
			snapshot.Ready = true
		}
	}
	return snapshot
}

func assignmentNumber(assignment topology.Assignment, selected []setup.ModelOption) int {
	for index, option := range selected {
		if assignment == option.Ref.Assignment() {
			return index + 1
		}
	}
	return 0
}

func hasSelectedOllama(selected []setup.ModelOption) bool {
	for _, option := range selected {
		if option.Ref.EndpointID == setup.OllamaEndpointID {
			return true
		}
	}
	return false
}

func validateDraft(draft *setup.Draft) error {
	selected := draft.SelectedModels()
	if len(selected) == 0 {
		return errors.New("select at least one model")
	}
	roles := draft.Config.Topology.Roles
	if assignmentNumber(roles.King, selected) == 0 {
		return errors.New("assign a selected model to King")
	}
	if assignmentNumber(roles.Worker, selected) == 0 {
		return errors.New("assign a selected model to Worker")
	}
	if draft.Config.CouncilEnabled && assignmentNumber(roles.Council, selected) == 0 {
		return errors.New("assign a selected model to Council or disable it")
	}
	if draft.Config.CouncilSize < 1 || draft.Config.CouncilSize > 9 {
		return errors.New("council size must be 1..9")
	}
	if draft.Config.WorkerConcurrency < 1 || draft.Config.WorkerConcurrency > 32 {
		return errors.New("worker concurrency must be 1..32")
	}
	return config.ValidateOllamaPortPlan(draft.Config)
}

func decodeArguments(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid arguments: trailing data")
	}
	return nil
}
