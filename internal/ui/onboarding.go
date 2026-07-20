package ui

import (
	"fmt"
	"strings"

	"github.com/callumny/kingdom/internal/setup"
)

func providersSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	body := []string{royalBrand.Render("Set up model providers"), ""}
	body = append(body, styledParagraph("Kingdom runs AI models entirely on your machine. Choose which local providers you want Kingdom to set up and manage.", 88, royalText)...)
	body = append(body, styledParagraph("You will choose up to three models next, then assign each one a role.", 88, royalMuted)...)
	body = append(body, "")
	if p.Scanning {
		body = append(body, royalCyan.Render("Checking local providers…"), "")
	}
	results := wf.Draft.Results
	if len(results) == 0 && p.Scanning {
		results = make([]setup.EndpointResult, len(wf.Draft.Config.Topology.Endpoints))
		for index, endpoint := range wf.Draft.Config.Topology.Endpoints {
			results[index].Endpoint = endpoint
		}
	}
	for index, result := range results {
		pointer := "  "
		if index == p.ProviderCursor {
			pointer = royalPointer.Render("› ")
		}
		checked := "[ ]"
		if wf.Draft.ProviderEnabled(result.Endpoint.ID) {
			checked = "[✓]"
		}
		status := providerStatus(result)
		if p.Scanning {
			status = royalCyan.Render("Checking…")
		}
		row := pointer + royalText.Render(fmt.Sprintf("%s %-12s", checked, result.Endpoint.Name)) + "  " + status
		body = append(body, row)
		if index == p.ProviderCursor {
			body = append(body, "    "+royalMuted.Render(providerDescription(result.Endpoint.ID)))
		}
	}
	if len(wf.Draft.Results) == 0 && !p.Scanning {
		body = append(body, royalMuted.Render("No providers answered. Use m to start or inspect local model runtimes."))
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	footer := "↑↓ Move   •   Space Toggle   •   Enter Continue   •   r Rescan"
	if p.Scanning {
		footer = "Checking providers…"
	}
	return body, royalMuted.Render(footer)
}

func modelsSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	selected := wf.Draft.SelectedModels()
	body := []string{royalBrand.Render("Choose your models"), ""}
	body = append(body, styledParagraph("Choose up to three models from any provider. A mix of sizes gives Kingdom flexible options for different jobs.", 88, royalMuted)...)
	body = append(body, "", royalCyan.Render(fmt.Sprintf("%d of %d selected", len(selected), setup.MaxSelectedModels)), "")
	for index, option := range wf.Draft.Catalog() {
		pointer := "  "
		if index == p.ModelCursor {
			pointer = royalPointer.Render("› ")
		}
		checked := "[ ]"
		if wf.Draft.IsModelSelected(option.Ref) {
			checked = "[✓]"
		}
		label := fmt.Sprintf("%-12s  %-30s", option.Endpoint.Name, option.Ref.ModelID)
		body = append(body, pointer+royalText.Render(checked+" "+label)+"  "+royalMuted.Render(modelMetadata(option)))
	}
	if len(wf.Draft.Catalog()) == 0 && !p.Scanning {
		body = append(body, royalMuted.Render("No ready models. Press m to start or load one from an installed provider."))
	}
	if p.Scanning {
		body = append(body, royalCyan.Render("Refreshing available models…"))
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	footer := "↑↓ Move   •   Space Toggle   •   Enter Assign roles   •   m Manage providers   •   Esc Back"
	if p.Scanning {
		footer = "Refreshing models…   •   Esc Back"
	}
	return body, royalMuted.Render(footer)
}

func modelMetadata(option setup.ModelOption) string {
	parts := make([]string, 0, 3)
	if option.ParameterSize != "" {
		parts = append(parts, option.ParameterSize)
	}
	if option.SizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("%.1f GB", float64(option.SizeBytes)/1_000_000_000))
	}
	if option.Quantization != "" {
		parts = append(parts, option.Quantization)
	}
	if len(parts) == 0 {
		return "size unknown"
	}
	return strings.Join(parts, " · ")
}

func providerStatus(result setup.EndpointResult) string {
	if result.Err != nil {
		return royalRed.Render("Unavailable")
	}
	if len(result.Models) == 0 {
		return royalGold.Render("Available · no models")
	}
	label := "models"
	if len(result.Models) == 1 {
		label = "model"
	}
	return royalGreen.Render(fmt.Sprintf("Available · %d %s", len(result.Models), label))
}

func providerDescription(endpointID string) string {
	switch endpointID {
	case "ollama-local":
		return "A simple general-purpose local model runtime."
	case "mlx-local":
		return "An Apple silicon-optimized runtime for MLX models."
	default:
		return "A custom local model provider."
	}
}
