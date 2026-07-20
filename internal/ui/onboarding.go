package ui

import (
	"fmt"
	"strings"

	"github.com/callumny/kingdom/internal/setup"
)

func welcomeSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	body := []string{royalBrand.Render("Welcome to Kingdom"), ""}
	body = append(body, styledParagraph("Kingdom runs AI models entirely on your machine, keeping your prompts local.", 88, royalText)...)
	body = append(body, styledParagraph("First choose where Kingdom should find your models, then select up to three. After that, Kingdom will help you assign each model a job.", 88, royalMuted)...)
	body = append(body, "", royalGold.Render("About model size"))
	body = append(body, styledParagraph("Larger models are generally more capable, but they use more RAM and are usually slower.", 88, royalText)...)
	body = append(body, styledParagraph("Smaller models are faster and work well for focused tasks or parallel work.", 88, royalText)...)
	body = append(body, "")
	if p.Scanning {
		body = append(body, royalCyan.Render("Looking for local model providers…"))
	} else {
		count := 0
		for _, result := range wf.Draft.Results {
			if len(result.Models) > 0 {
				count++
			}
		}
		body = append(body, royalGreen.Render(fmt.Sprintf("Found %d available provider(s)", count)))
	}
	return body, royalMuted.Render("Enter Begin setup   •   r Rescan   •   q Quit")
}

func providersSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	body := []string{royalBrand.Render("Set up model providers"), ""}
	body = append(body, styledParagraph("Kingdom checks every local provider. Ready providers automatically contribute models to the next step.", 88, royalMuted)...)
	body = append(body, "")
	if p.Scanning {
		body = append(body, royalCyan.Render("Checking local providers…"), "")
	}
	for index, result := range wf.Draft.Results {
		pointer := "  "
		if index == p.ProviderCursor {
			pointer = royalPointer.Render("› ")
		}
		row := pointer + royalText.Render(fmt.Sprintf("%-12s", result.Endpoint.Name)) + "  " + providerStatus(result)
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
	footer := "↑↓ Inspect   •   Enter View models   •   m Manage providers   •   Esc Back"
	if p.Scanning {
		footer = "Checking providers…   •   Esc Back"
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
	case "lm-studio-local":
		return "A desktop application for discovering and running local models."
	case "mlx-local":
		return "An Apple silicon-optimized runtime for MLX models."
	default:
		return "A custom local model provider."
	}
}
