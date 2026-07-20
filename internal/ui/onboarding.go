package ui

import (
	"fmt"

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
	body := []string{royalBrand.Render("Choose your model providers"), ""}
	body = append(body, styledParagraph("Select one or more places where Kingdom should find and run local models.", 88, royalMuted)...)
	body = append(body, "")
	if p.Scanning {
		body = append(body, royalCyan.Render("Checking local providers…"), "")
	}
	for index, result := range wf.Draft.Results {
		pointer := "  "
		if index == p.ProviderCursor {
			pointer = royalPointer.Render("› ")
		}
		checked := "[ ]"
		if p.SelectedProviders[result.Endpoint.ID] {
			checked = "[✓]"
		}
		row := pointer + royalText.Render(checked+" "+fmt.Sprintf("%-12s", result.Endpoint.Name)) + "  " + providerStatus(result)
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
	footer := "↑↓ Move   •   Space Toggle   •   Enter Continue   •   m Manage models   •   Esc Back"
	if p.Scanning {
		footer = "Checking providers…   •   Esc Back"
	}
	return body, royalMuted.Render(footer)
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

func providerIsSelected(selected map[string]bool, endpointID string) bool {
	return selected == nil || selected[endpointID]
}
