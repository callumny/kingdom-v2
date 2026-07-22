package ui

import (
	"fmt"
	"strings"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
)

func wizardSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	body := []string{
		royalBrand.Render("Wizard"),
		royalMuted.Render("A short conversation to finish your Kingdom."),
	}
	if p.WizardModel != "" {
		body = append(body, royalBadge.Render(p.WizardModel))
	}
	body = append(body, "")
	if p.WizardPreparing {
		body = append(body, royalMuted.Render("Starting the local Wizard model in the background…"), "")
	}
	start := max(0, len(p.WizardMessages)-4)
	for _, message := range p.WizardMessages[start:] {
		if strings.HasPrefix(message, "You: ") {
			body = append(body, royalText.Render(message))
		} else {
			body = append(body, royalCyan.Render(message))
		}
	}
	body = append(body, "", royalGold.Render("Proposed Kingdom"))
	roles := wf.Draft.Config.Topology.Roles
	body = append(body,
		fmt.Sprintf("King:       %s", assignmentLabel(roles.King, wf)),
		fmt.Sprintf("Worker:     %s", assignmentLabel(roles.Worker, wf)),
	)
	if wf.Draft.Config.CouncilEnabled {
		body = append(body, fmt.Sprintf("Council:    %s · %d members", assignmentLabel(roles.Council, wf), wf.Draft.Config.CouncilSize))
	} else {
		body = append(body, "Council:    "+royalMuted.Render("disabled"))
	}
	body = append(body, fmt.Sprintf("Concurrency: %d Workers", wf.Draft.Config.WorkerConcurrency))
	if hasManagedOllamaSelection(wf) {
		mode := "shared server"
		if wf.Draft.Config.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
			mode = "separate servers"
		}
		body = append(body, fmt.Sprintf("Ollama:     %s · base port %d", mode, wf.Draft.Config.Providers.Ollama.Port))
	}
	if hasManagedMLXSelection(wf) {
		body = append(body, fmt.Sprintf("MLX:        one server per model · base port %d", wf.Draft.Config.Providers.MLX.Port))
	}
	body = append(body, "")
	if p.WizardBusy {
		body = append(body, royalCyan.Render("Wizard is thinking…"))
	} else {
		body = append(body, royalText.Render("Ask: "+p.WizardInput+"▏"))
	}
	footer := "Type a question or change   •   Ctrl+Enter Send   •   Esc Back"
	if p.WizardReady {
		footer = "Ctrl+Enter Send   •   Enter Apply & launch   •   Esc Back"
	}
	if p.WizardApplying {
		footer = "Validating and saving setup…"
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	return body, royalMuted.Render(footer)
}

func hasManagedMLXSelection(wf *setup.Workflow) bool {
	for _, option := range wf.Draft.SelectedModels() {
		if option.Ref.EndpointID == setup.MLXEndpointID {
			return true
		}
	}
	return false
}

func hasManagedOllamaSelection(wf *setup.Workflow) bool {
	for _, option := range wf.Draft.SelectedModels() {
		if option.Ref.EndpointID == setup.OllamaEndpointID {
			return true
		}
	}
	return false
}
