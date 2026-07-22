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
		{Name: "enable_council", Description: "Enable or disable the optional Council. Enabling reuses the proposed King model when no Council model is assigned.", Parameters: schema(`{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"],"additionalProperties":false}`)},
		{Name: "assign_model", Description: "Assign a selected model to one role using its exact displayed model name and optional provider.", Parameters: schema(`{"type":"object","properties":{"role":{"enum":["king","worker","council"]},"model":{"type":"string","minLength":1},"provider":{"enum":["ollama","mlx"]}},"required":["role","model"],"additionalProperties":false}`)},
		{Name: "swap_roles", Description: "Atomically swap the models assigned to two enabled roles.", Parameters: schema(`{"type":"object","properties":{"first":{"enum":["king","worker","council"]},"second":{"enum":["king","worker","council"]}},"required":["first","second"],"additionalProperties":false}`)},
		{Name: "set_council_size", Description: "Set the number of Council reviewers from 1 to 9.", Parameters: schema(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":9}},"required":["count"],"additionalProperties":false}`)},
		{Name: "set_worker_concurrency", Description: "Set concurrent Workers from 1 to 32.", Parameters: schema(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":32}},"required":["count"],"additionalProperties":false}`)},
		{Name: "set_ollama_server_mode", Description: "Use separate Ollama servers per model or one shared server.", Parameters: schema(`{"type":"object","properties":{"mode":{"enum":["separate","shared"]}},"required":["mode"],"additionalProperties":false}`)},
		{Name: "set_provider_port", Description: "Set the base loopback port for an enabled Ollama or MLX provider.", Parameters: schema(`{"type":"object","properties":{"provider":{"enum":["ollama","mlx"]},"port":{"type":"integer","minimum":1,"maximum":65535}},"required":["provider","port"],"additionalProperties":false}`)},
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
	appliedConfig   config.Config
	applied         bool
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

func (s *Session) PrepareDefaults() error {
	if s == nil || s.draft == nil {
		return errors.New("setup draft is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draft.ApplyRoleSuggestions()
}

func (s *Session) HasModel(ref setup.ModelRef) bool {
	if s == nil || s.draft == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, option := range s.draft.SelectedModels() {
		if option.Ref == ref {
			return true
		}
	}
	return false
}

func (s *Session) AppliedConfig() (config.Config, bool) {
	if s == nil {
		return config.Config{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appliedConfig, s.applied
}

func (s *Session) Ready() bool {
	if s == nil || s.draft == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return validateDraft(s.draft) == nil
}

func (s *Session) ChangeSummary(toolNames []string) string {
	if s == nil || s.draft == nil {
		return "Setup updated."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		switch name {
		case "enable_council":
			if s.draft.Config.CouncilEnabled {
				parts = append(parts, "Council enabled.")
			} else {
				parts = append(parts, "Council disabled.")
			}
		case "assign_model":
			parts = append(parts, s.roleAssignmentsSummary())
		case "swap_roles":
			parts = append(parts, "Role models swapped.")
		case "set_council_size":
			parts = append(parts, fmt.Sprintf("Council members set to %d.", s.draft.Config.CouncilSize))
		case "set_worker_concurrency":
			parts = append(parts, fmt.Sprintf("Concurrent workers set to %d.", s.draft.Config.WorkerConcurrency))
		case "set_ollama_server_mode":
			mode := "shared"
			if s.draft.Config.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
				mode = "separate"
			}
			parts = append(parts, "Ollama servers set to "+mode+".")
		case "set_provider_port":
			parts = append(parts, "Provider port updated.")
		}
	}
	if len(parts) == 0 {
		return "Setup updated."
	}
	return strings.Join(parts, " ")
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
			s.setCouncilEnabled(arguments.Enabled)
		}
	case "assign_model":
		var arguments struct {
			Role     string `json:"role"`
			Model    string `json:"model"`
			Provider string `json:"provider"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			err = s.assign(arguments.Role, arguments.Model, arguments.Provider)
		}
	case "swap_roles":
		var arguments struct {
			First  string `json:"first"`
			Second string `json:"second"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			err = s.swapRoles(arguments.First, arguments.Second)
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
	case "set_provider_port":
		var arguments struct {
			Provider string `json:"provider"`
			Port     int    `json:"port"`
		}
		if err = decodeArguments(call.Arguments, &arguments); err == nil {
			err = s.setProviderPort(arguments.Provider, arguments.Port)
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

func (s *Session) setCouncilEnabled(enabled bool) {
	if !enabled {
		s.draft.SetCouncilEnabled(false)
		return
	}
	selected := s.draft.SelectedModels()
	roles := s.draft.Config.Topology.Roles
	if assignmentNumber(roles.Council, selected) > 0 {
		s.draft.SetCouncilEnabled(true)
		return
	}
	if assignmentNumber(roles.King, selected) > 0 {
		s.draft.AssignCouncil(roles.King)
		return
	}
	if len(selected) > 0 {
		s.draft.AssignCouncil(selected[0].Ref.Assignment())
	}
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
	s.appliedConfig = s.draft.Config
	s.applied = true
	return nil
}

func (s *Session) assign(role, model, provider string) error {
	selected := s.draft.SelectedModels()
	model = strings.TrimSpace(model)
	provider = strings.ToLower(strings.TrimSpace(provider))
	var matches []setup.ModelOption
	for _, option := range selected {
		if !strings.EqualFold(option.Ref.ModelID, model) || (provider != "" && !modelProviderMatches(option, provider)) {
			continue
		}
		matches = append(matches, option)
	}
	if len(matches) == 0 {
		return fmt.Errorf("selected model %q was not found", model)
	}
	if len(matches) > 1 {
		return fmt.Errorf("selected model %q exists in multiple providers; include provider", model)
	}
	assignment := matches[0].Ref.Assignment()
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

func modelProviderMatches(option setup.ModelOption, provider string) bool {
	candidates := []string{option.Endpoint.Name, option.Ref.EndpointID, strings.TrimSuffix(option.Ref.EndpointID, "-local")}
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate), provider) {
			return true
		}
	}
	return false
}

func (s *Session) assignmentLabel(assignment topology.Assignment) string {
	if !assignment.Complete() {
		return "disabled"
	}
	for _, option := range s.draft.SelectedModels() {
		if option.Ref.Assignment() == assignment {
			provider := option.Endpoint.Name
			if provider == "" {
				provider = option.Ref.EndpointID
			}
			return provider + " / " + option.Ref.ModelID
		}
	}
	return assignment.EndpointID + " / " + assignment.Model
}

func (s *Session) roleAssignmentsSummary() string {
	roles := s.draft.Config.Topology.Roles
	return fmt.Sprintf(
		"Role assignments updated: King uses %s, Worker uses %s, Council uses %s.",
		s.assignmentLabel(roles.King),
		s.assignmentLabel(roles.Worker),
		s.assignmentLabel(roles.Council),
	)
}

func (s *Session) swapRoles(first, second string) error {
	return s.draft.SwapRoles(first, second)
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

func (s *Session) setProviderPort(provider string, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("provider port must be 1..65535")
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "ollama":
		if !s.draft.Config.Providers.Ollama.Enabled {
			return errors.New("Ollama is not enabled")
		}
		previous := s.draft.Config.Providers.Ollama.Port
		s.draft.Config.Providers.Ollama.Port = port
		if err := config.ValidateRuntimePortPlan(s.draft.Config); err != nil {
			s.draft.Config.Providers.Ollama.Port = previous
			return err
		}
	case "mlx":
		if !s.draft.Config.Providers.MLX.Enabled {
			return errors.New("MLX is not enabled")
		}
		previous := s.draft.Config.Providers.MLX.Port
		s.draft.Config.Providers.MLX.Port = port
		if err := config.ValidateRuntimePortPlan(s.draft.Config); err != nil {
			s.draft.Config.Providers.MLX.Port = previous
			return err
		}
	default:
		return errors.New("provider must be ollama or mlx")
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
	OllamaPort        int             `json:"ollama_port,omitempty"`
	MLXPort           int             `json:"mlx_port,omitempty"`
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
		snapshot.OllamaPort = s.draft.Config.Providers.Ollama.Port
		snapshot.OllamaServerMode = "shared"
		if s.draft.Config.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
			snapshot.OllamaServerMode = "separate"
		}
	}
	if hasSelectedProvider(selected, setup.MLXEndpointID) {
		snapshot.MLXPort = s.draft.Config.Providers.MLX.Port
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
	return hasSelectedProvider(selected, setup.OllamaEndpointID)
}

func hasSelectedProvider(selected []setup.ModelOption, endpointID string) bool {
	for _, option := range selected {
		if option.Ref.EndpointID == endpointID {
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
	return config.ValidateRuntimePortPlan(draft.Config)
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
