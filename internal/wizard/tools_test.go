package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
)

func TestToolDefinitionsAreSmallAndSinglePurpose(t *testing.T) {
	want := []string{"inspect_setup", "enable_council", "assign_model", "set_council_size", "set_worker_concurrency", "set_ollama_server_mode", "set_provider_port", "preview_setup", "apply_setup"}
	definitions := ToolDefinitions()
	if len(definitions) != len(want) {
		t.Fatalf("definitions=%+v", definitions)
	}
	for index, name := range want {
		if definitions[index].Name != name || definitions[index].Description == "" || len(definitions[index].Parameters) == 0 {
			t.Fatalf("definition %d=%+v", index, definitions[index])
		}
	}
}

func TestInspectSetupNumbersSelectedModels(t *testing.T) {
	session := NewSession(wizardDraft(t))
	result := session.Run(context.Background(), call("inspect_setup", `{}`))
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	for _, want := range []string{`"number":1`, `"provider":"Ollama"`, `"model":"large"`, `"number":2`, `"provider":"MLX"`, `"model":"small"`} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("inspect missing %q: %s", want, result.Output)
		}
	}
}

func TestSetupToolsMutateOnlyTheirOwnSetting(t *testing.T) {
	draft := wizardDraft(t)
	session := NewSession(draft)

	for _, item := range []struct {
		name string
		args string
	}{
		{"assign_model", `{"role":"king","model_number":1}`},
		{"assign_model", `{"role":"worker","model_number":2}`},
		{"enable_council", `{"enabled":true}`},
		{"assign_model", `{"role":"council","model_number":2}`},
		{"set_council_size", `{"count":2}`},
		{"set_worker_concurrency", `{"count":4}`},
		{"set_ollama_server_mode", `{"mode":"separate"}`},
		{"set_provider_port", `{"provider":"mlx","port":13000}`},
	} {
		if result := session.Run(context.Background(), call(item.name, item.args)); result.Error != "" {
			t.Fatalf("%s: %s", item.name, result.Error)
		}
	}

	cfg := draft.Config
	if cfg.Topology.Roles.King.Model != "large" || cfg.Topology.Roles.Worker.Model != "small" || cfg.Topology.Roles.Council.Model != "small" {
		t.Fatalf("roles=%+v", cfg.Topology.Roles)
	}
	if !cfg.CouncilEnabled || cfg.CouncilSize != 2 || cfg.WorkerConcurrency != 4 || cfg.Providers.Ollama.PortMode != config.OllamaDedicatedPorts || cfg.Providers.MLX.Port != 13000 {
		t.Fatalf("settings=%+v", cfg)
	}
	preview := session.Run(context.Background(), call("preview_setup", `{}`))
	if preview.Error != "" || !strings.Contains(preview.Output, `"ready":true`) {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestSetupToolsRejectInvalidOrCombinedArguments(t *testing.T) {
	session := NewSession(wizardDraft(t))
	for _, item := range []struct {
		name string
		args string
	}{
		{"assign_model", `{"role":"king","model_number":3}`},
		{"assign_model", `{"role":"queen","model_number":1}`},
		{"set_council_size", `{"count":0}`},
		{"set_worker_concurrency", `{"count":33}`},
		{"set_ollama_server_mode", `{"mode":"automatic"}`},
		{"set_provider_port", `{"provider":"mlx","port":70000}`},
		{"set_provider_port", `{"provider":"unknown","port":8080}`},
		{"enable_council", `{"enabled":true,"count":2}`},
		{"unknown", `{}`},
	} {
		if result := session.Run(context.Background(), call(item.name, item.args)); result.Error == "" {
			t.Fatalf("%s accepted %s", item.name, item.args)
		}
	}
}

func TestDisablingCouncilClearsItsAssignment(t *testing.T) {
	draft := wizardDraft(t)
	draft.Config.CouncilEnabled = true
	draft.Config.Topology.Roles.Council = topology.Assignment{EndpointID: setup.OllamaEndpointID, Model: "large"}
	session := NewSession(draft)
	if result := session.Run(context.Background(), call("enable_council", `{"enabled":false}`)); result.Error != "" {
		t.Fatal(result.Error)
	}
	if draft.Config.CouncilEnabled || draft.Config.Topology.Roles.Council.Complete() {
		t.Fatalf("council not cleared: %+v", draft.Config)
	}
}

func TestEnablingCouncilUsesTheProposedKingWhenNoCouncilIsAssigned(t *testing.T) {
	draft := wizardDraft(t)
	if err := draft.ApplyRoleSuggestions(); err != nil {
		t.Fatal(err)
	}
	if draft.Config.CouncilEnabled {
		t.Fatal("two-model defaults should start without a Council")
	}
	session := NewSession(draft)
	if result := session.Run(context.Background(), call("enable_council", `{"enabled":true}`)); result.Error != "" {
		t.Fatal(result.Error)
	}
	if !draft.Config.CouncilEnabled || draft.Config.Topology.Roles.Council != draft.Config.Topology.Roles.King {
		t.Fatalf("Council did not reuse proposed King: %+v", draft.Config.Topology.Roles)
	}
	preview := session.Run(context.Background(), call("preview_setup", `{}`))
	if preview.Error != "" || !strings.Contains(preview.Output, `"ready":true`) {
		t.Fatalf("enabled Council left invalid setup: %+v", preview)
	}
}

func TestApplySetupRequiresConfirmationAndValidDraft(t *testing.T) {
	draft := wizardDraft(t)
	var saved config.Config
	session := NewSessionWithSave(draft, func(next config.Config) error {
		saved = next
		return nil
	})
	if result := session.Run(context.Background(), call("apply_setup", `{}`)); !strings.Contains(result.Error, "confirmation") {
		t.Fatalf("unconfirmed apply=%+v", result)
	}
	session.AuthorizeApply()
	if result := session.Run(context.Background(), call("apply_setup", `{}`)); !strings.Contains(result.Error, "assign") {
		t.Fatalf("invalid apply=%+v", result)
	}
	for _, item := range []struct {
		role  string
		model int
	}{{"king", 1}, {"worker", 2}} {
		arguments := fmt.Sprintf(`{"role":%q,"model_number":%d}`, item.role, item.model)
		if result := session.Run(context.Background(), call("assign_model", arguments)); result.Error != "" {
			t.Fatal(result.Error)
		}
	}
	draft.SetCouncilEnabled(false)
	session.AuthorizeApply()
	if result := session.Run(context.Background(), call("apply_setup", `{}`)); result.Error != "" || !strings.Contains(result.Output, `"applied":true`) {
		t.Fatalf("confirmed apply=%+v", result)
	}
	if saved.Topology.Roles.King.Model != "large" || saved.Topology.Roles.Worker.Model != "small" {
		t.Fatalf("saved=%+v", saved)
	}
	if result := session.Run(context.Background(), call("apply_setup", `{}`)); !strings.Contains(result.Error, "confirmation") {
		t.Fatalf("authorization was reusable: %+v", result)
	}
}

func call(name, arguments string) tools.Call {
	return tools.Call{ID: "test", Name: name, Arguments: json.RawMessage(arguments)}
}

func wizardDraft(t *testing.T) *setup.Draft {
	t.Helper()
	cfg := config.Default()
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.MLX.Enabled = true
	cfg.Topology.Endpoints = []topology.Endpoint{
		{ID: setup.OllamaEndpointID, Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"},
		{ID: setup.MLXEndpointID, Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"},
	}
	draft := setup.NewDraft(cfg, nil)
	options := []setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: setup.OllamaEndpointID, ModelID: "large"}, Endpoint: cfg.Topology.Endpoints[0], Installed: true, ParameterSize: "14B"},
		{Ref: setup.ModelRef{EndpointID: setup.MLXEndpointID, ModelID: "small"}, Endpoint: cfg.Topology.Endpoints[1], Installed: true, ParameterSize: "8B"},
	}
	draft.ReplaceCatalog(options)
	for _, option := range options {
		if err := draft.ToggleModel(option.Ref); err != nil {
			t.Fatal(err)
		}
	}
	return &draft
}
