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
		status := providerStatus(result, wf.Draft.ProviderReady(result.Endpoint.ID))
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
		body = append(body, royalMuted.Render("No providers answered. Press r to check again, or select a provider and press i to install it."))
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	if p.ProviderNotice != "" {
		body = append(body, "", royalGreen.Render(p.ProviderNotice))
	}
	footer := "↑↓ Move   •   Space Toggle   •   i Install   •   Enter Continue   •   r Rescan"
	if p.ProviderConfirming {
		body = append(body, "", royalGold.Render("Install this provider from its official source? y confirm   n cancel"))
		footer = "y Confirm installation   •   n Cancel"
	}
	if p.ProviderInstalling {
		body = append(body, "", royalCyan.Render(p.ProviderProgress.Message), providerProgressBar(p.ProviderProgress.Completed, p.ProviderProgress.Total), royalMuted.Render("Provider setup can take a few minutes."))
		footer = "Installation in progress…"
	}
	if p.Scanning {
		footer = "Checking providers…"
	}
	return body, royalMuted.Render(footer)
}

func providerProgressBar(completed, total int) string {
	if total < 1 {
		total = 1
	}
	if completed < 0 {
		completed = 0
	}
	if completed > total {
		completed = total
	}
	const width = 28
	filled := completed * width / total
	percent := completed * 100 / total
	return royalGreen.Render("["+strings.Repeat("█", filled)) + royalMuted.Render(strings.Repeat("░", width-filled)+fmt.Sprintf("] %d%%", percent))
}

func modelsSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	selected := wf.Draft.SelectedModels()
	body := []string{royalBrand.Render("Choose your models"), ""}
	body = append(body, styledParagraph("Choose up to three models from any provider. A mix of sizes gives Kingdom flexible options for different jobs.", 88, royalMuted)...)
	if p.ModelDownloadConfirming {
		pending := wf.Draft.PendingDownloads()
		title := "Download selected models?"
		if len(pending) == 1 {
			title = "Download selected model?"
		}
		body = []string{royalBrand.Render(title), ""}
		body = append(body, styledParagraph("Kingdom will download only the models you do not already have. Downloads start after you confirm; installed models are left untouched.", 88, royalMuted)...)
		body = append(body, "")
		for _, option := range pending {
			body = append(body, royalText.Render(fmt.Sprintf("• %-12s  %s", option.Endpoint.Name, option.Ref.ModelID)))
		}
		body = append(body, "", royalMuted.Render("You can assign roles while these models download."))
		return body, royalMuted.Render("y / Enter Confirm and continue   •   n / Esc Back")
	}
	if p.ModelInventoryLoading {
		body = append(body, "", royalCyan.Render("Checking installed models across your selected providers…"))
		return body, royalMuted.Render("Checking installed models…   •   Esc Back")
	}
	searchPrompt := "Press / to search Ollama and MLX"
	if p.ModelSearchActive || p.ModelQuery != "" {
		searchPrompt = "Search: " + p.ModelQuery + "▏"
	}
	body = append(body, "", royalText.Render(searchPrompt))
	if p.ModelSearching {
		body = append(body, royalCyan.Render("Searching Ollama and MLX…"))
	}
	if p.ModelSearchWarning != "" {
		body = append(body, royalGold.Render(p.ModelSearchWarning))
	}
	body = append(body, "", royalCyan.Render(fmt.Sprintf("%d of %d selected", len(selected), setup.MaxSelectedModels)), "")
	catalog := wf.Draft.Catalog()
	start, end := modelWindow(len(catalog), p.ModelCursor)
	for offset, option := range catalog[start:end] {
		index := start + offset
		pointer := "  "
		if index == p.ModelCursor {
			pointer = royalPointer.Render("› ")
		}
		checked := "[ ]"
		if wf.Draft.IsModelSelected(option.Ref) {
			checked = "[✓]"
		}
		label := fmt.Sprintf("%-12s  %-30s", option.Endpoint.Name, option.Ref.ModelID)
		state := "Download"
		if option.Installed {
			state = "Installed"
		}
		body = append(body, pointer+royalText.Render(checked+" "+label)+"  "+royalMuted.Render(state+" · "+modelMetadata(option)))
	}
	if len(catalog) > modelWindowSize {
		body = append(body, "", royalMuted.Render(fmt.Sprintf("Showing %d–%d of %d", start+1, end, len(catalog))))
	}
	if len(catalog) == 0 && !p.Scanning {
		body = append(body, royalMuted.Render("No installed models found. Press / to search Ollama and MLX."))
	}
	if p.Scanning {
		body = append(body, royalCyan.Render("Refreshing available models…"))
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	footer := "/ Search   •   ↑↓ Move   •   Space Toggle   •   Enter Continue   •   Esc Back"
	if p.ModelSearchActive {
		footer = "Type to search   •   ↑↓ Move   •   Enter Finish search   •   Esc Clear"
	}
	if p.Scanning {
		footer = "Refreshing models…   •   Esc Back"
	}
	return body, royalMuted.Render(footer)
}

const modelWindowSize = 8

func modelWindow(total, cursor int) (start, end int) {
	if total <= modelWindowSize {
		return 0, total
	}
	start = cursor - modelWindowSize/2
	if start < 0 {
		start = 0
	}
	end = start + modelWindowSize
	if end > total {
		end = total
		start = end - modelWindowSize
	}
	return start, end
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

func providerStatus(result setup.EndpointResult, ready bool) string {
	if ready {
		if len(result.Models) == 0 {
			return royalGreen.Render("Ready")
		}
		label := "models"
		if len(result.Models) == 1 {
			label = "model"
		}
		return royalGreen.Render(fmt.Sprintf("Ready · %d %s", len(result.Models), label))
	}
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
