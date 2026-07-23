package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/callumny/kingdom/internal/localmodels"
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

func modelsSetupView(wf *setup.Workflow, p Presentation, height int) ([]string, string) {
	selected := wf.Draft.SelectedModels()
	if p.ModelRemoveConfirming {
		return modelRemoveConfirmation(wf, p.ModelRemoveTarget)
	}
	if p.ModelRemoveActive {
		provider := modelProviderLabel(p.ModelRemoveTarget)
		body := []string{
			royalBrand.Render("Uninstalling model"),
			"",
			royalCyan.Render(provider + " · " + p.ModelRemoveTarget.Ref.ModelID),
			royalMuted.Render("Removing downloaded files…"),
		}
		return body, royalMuted.Render("Please wait…")
	}
	if p.ModelDownloadConfirming {
		return modelDownloadConfirmation(wf)
	}
	if p.ModelDownloadActive {
		status := p.ModelDownloadProgress.Status
		if status == "" {
			status = "Preparing model download"
		}
		provider := downloadProviderLabel(p.ModelDownloadProgress.Provider)
		position := "Downloading selected model"
		if p.ModelDownloadPosition > 0 && p.ModelDownloadCount > 0 {
			position = fmt.Sprintf("Model %d of %d", p.ModelDownloadPosition, p.ModelDownloadCount)
		}
		body := []string{
			royalBrand.Render("Preparing your models"),
			"",
			royalGold.Render(position),
			royalText.Render(provider + " · " + p.ModelDownloadProgress.Model),
			"",
			royalCyan.Render(fmt.Sprintf("%s · %d%%", status, p.ModelDownloadProgress.Percent)),
			providerProgressBar(p.ModelDownloadProgress.Percent, 100),
			royalText.Render(downloadSizeSummary(p.ModelDownloadProgress)),
			royalText.Render(downloadTimeSummary(p.ModelDownloadProgress)),
			"",
		}
		body = append(body, styledParagraph("When every download is ready, Kingdom opens the Wizard automatically to complete setup.", 88, royalMuted)...)
		return body, royalMuted.Render("Downloading selected models…")
	}
	searching := p.ModelSearchActive || p.ModelQuery != ""
	title := "Choose your models"
	description := "Installed Ollama and MLX models appear together. Select up to three; a mix of sizes works well for different jobs."
	if searching {
		title = "Search models"
		description = "One fuzzy search covers both selected providers. Every result keeps its provider and installation state visible."
	}
	body := []string{royalBrand.Render(title), ""}
	body = append(body, styledParagraph(description, 88, royalMuted)...)
	if wf.Draft.Config.Providers.MLX.Enabled {
		body = append(body, "")
		body = append(body, styledParagraph("Tip: MLX models generally run faster on Apple silicon and are optimized for compatible Macs.", 88, royalCyan)...)
	}
	if p.ModelDownloadError != "" {
		body = append(body, "", royalRed.Render("Download failed: "+p.ModelDownloadError))
		body = append(body, royalMuted.Render("Press Enter to review and retry the missing model downloads."))
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
	if p.ModelRemoveNotice != "" {
		body = append(body, royalGreen.Render(p.ModelRemoveNotice), "")
	}
	catalog := wf.Draft.Catalog()
	if len(selected) == setup.MaxSelectedModels {
		body = append(body, selectionSummary(selected)...)
	}
	if searching && p.ModelQuery != "" {
		body = append(body, searchResultsSummary(catalog, p.ModelQuery))
		body = append(body, modelTableHeader())
	} else {
		body = append(body, installedResultsSummary(catalog))
	}
	start, end := modelWindowForHeight(len(catalog), p.ModelCursor, height)
	if searching && p.ModelQuery != "" {
		for offset, option := range catalog[start:end] {
			index := start + offset
			body = append(body, modelOptionRow(option, index == p.ModelCursor, wf.Draft.IsModelSelected(option.Ref)))
		}
	} else {
		body = append(body, groupedModelRows(catalog, start, end, p.ModelCursor, wf.Draft.IsModelSelected)...)
		if p.ModelPopularLoading {
			body = append(body, "", royalText.Render("Popular downloads"))
			body = append(body, royalCyan.Render("Finding popular Ollama and MLX models…"))
		}
		if p.ModelPopularWarning != "" {
			body = append(body, "", royalGold.Render(p.ModelPopularWarning))
			body = append(body, royalMuted.Render("Popular models are optional. Press / to search for any model."))
		}
	}
	if start > 0 || end < len(catalog) {
		body = append(body, "", royalMuted.Render(fmt.Sprintf("Showing %d–%d of %d", start+1, end, len(catalog))))
	}
	if len(catalog) == 0 && !p.Scanning {
		body = append(body, royalMuted.Render("No installed models found. Press / to search Ollama and MLX."))
	} else if searching {
		body = append(body, "", royalMuted.Render("Installed fuzzy matches rank first. Up to ten online matches per provider follow."))
	} else {
		body = append(body, "", royalMuted.Render("Installed models are always shown first."))
	}
	if p.Scanning {
		body = append(body, royalCyan.Render("Refreshing available models…"))
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	footer := "/ Search  •  Space Select  •  d Uninstall  •  Enter Continue  •  Esc Back"
	if p.ModelSearchActive {
		footer = "Type to filter   •   ↑↓ Move   •   Space Select   •   Enter Finish search   •   Esc Clear"
	}
	if p.Scanning {
		footer = "Refreshing models…   •   Esc Back"
	}
	return body, royalMuted.Render(footer)
}

func downloadProviderLabel(provider localmodels.Kind) string {
	switch provider {
	case localmodels.KindOllama:
		return "Ollama"
	case localmodels.KindMLX:
		return "MLX"
	default:
		return "Local model"
	}
}

func modelRemoveConfirmation(wf *setup.Workflow, option setup.ModelOption) ([]string, string) {
	provider := modelProviderLabel(option)
	body := []string{
		royalBrand.Render("Uninstall " + provider + " model?"),
		"",
		modelOptionRow(option, false, wf.Draft.IsModelSelected(option.Ref)),
		"",
	}
	body = append(body, styledParagraph("This permanently removes the downloaded model files from this machine.", 88, royalMuted)...)
	if wf.Draft.IsModelSelected(option.Ref) {
		body = append(body, royalGold.Render("It will also be removed from your current selection."))
	}
	body = append(body, royalMuted.Render("If it was assigned previously, choose a replacement before leaving setup."))
	body = append(body, royalMuted.Render("A running model may remain in memory until its provider process stops."))
	return body, royalMuted.Render("Enter / y Uninstall   •   Esc / n Cancel")
}

func modelProviderLabel(option setup.ModelOption) string {
	if option.Endpoint.Name != "" {
		return option.Endpoint.Name
	}
	switch option.Ref.EndpointID {
	case setup.OllamaEndpointID:
		return "Ollama"
	case setup.MLXEndpointID:
		return "MLX"
	default:
		return option.Ref.EndpointID
	}
}

func modelDownloadConfirmation(wf *setup.Workflow) ([]string, string) {
	pending := wf.Draft.PendingDownloads()
	title := "Download selected models?"
	if len(pending) == 1 {
		title = "Download selected model?"
	}
	body := []string{royalBrand.Render(title), ""}
	body = append(body, styledParagraph("Kingdom will download only the missing models. Your installed choices stay untouched.", 88, royalMuted)...)
	body = append(body, "")
	for _, option := range pending {
		body = append(body, modelOptionRow(option, false, true))
	}
	body = append(body,
		"",
		royalGold.Render("What happens next"),
		royalMuted.Render("Kingdom waits for every download, then opens the Wizard to complete setup."),
	)
	return body, royalMuted.Render("Enter / y Confirm and continue   •   Esc / n Back")
}

func downloadSizeSummary(progress localmodels.DownloadProgress) string {
	if progress.TotalBytes > 0 {
		return fmt.Sprintf("%s / %s", formatDownloadBytes(progress.DownloadedBytes), formatDownloadBytes(progress.TotalBytes))
	}
	if progress.DownloadedBytes > 0 {
		return formatDownloadBytes(progress.DownloadedBytes) + " downloaded · total size calculating…"
	}
	return "Download size calculating…"
}

func downloadTimeSummary(progress localmodels.DownloadProgress) string {
	if progress.BytesPerSecond <= 0 {
		return "Speed and time remaining calculating…"
	}
	summary := formatDownloadBytes(progress.BytesPerSecond) + "/s"
	if progress.Provider == localmodels.KindMLX {
		summary = "Estimated " + summary
	}
	if progress.ETA > 0 {
		summary += " · " + formatDownloadETA(progress.ETA) + " remaining"
	}
	return summary
}

func formatDownloadBytes(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unit := 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatDownloadETA(duration time.Duration) string {
	duration = duration.Round(time.Second)
	if duration < time.Second {
		return "<1s"
	}
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	seconds := int(duration / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func installedResultsSummary(catalog []setup.ModelOption) string {
	installed := make([]setup.ModelOption, 0, len(catalog))
	for _, option := range catalog {
		if option.Installed {
			installed = append(installed, option)
		}
	}
	return royalText.Render("Installed models") + "  " + royalMuted.Render(fmt.Sprintf("%d found across %d providers", len(installed), providerCount(installed)))
}

func searchResultsSummary(catalog []setup.ModelOption, query string) string {
	providers := providerLabels(catalog)
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		parts = append(parts, royalBadge.Render(provider+" ✓"))
	}
	label := fmt.Sprintf("Results for “%s”", query)
	return royalText.Render(label) + "  " + strings.Join(parts, " ") + "  " + royalMuted.Render(fmt.Sprintf("%d matches", len(catalog)))
}

func selectionSummary(selected []setup.ModelOption) []string {
	sizes := make([]string, 0, len(selected))
	for _, option := range selected {
		if option.ParameterSize != "" {
			sizes = append(sizes, option.ParameterSize)
		}
	}
	detail := "Bigger models are generally stronger, but slower and use more RAM."
	if len(sizes) == len(selected) {
		detail = "A useful spread: " + strings.Join(sizes, " · ") + ". " + detail
	}
	pending := 0
	for _, option := range selected {
		if !option.Installed {
			pending++
		}
	}
	lines := []string{royalGold.Render(fmt.Sprintf("%d models selected", len(selected)))}
	lines = append(lines, styledParagraph(detail, 88, royalMuted)...)
	if pending > 0 {
		label := fmt.Sprintf("%d downloads required", pending)
		if pending == 1 {
			label = "One download required"
		}
		lines = append(lines, royalPending.Render(label)+"  "+royalMuted.Render("Continuing asks for confirmation before using the network or disk."), "")
	}
	return lines
}

func modelOptionRow(option setup.ModelOption, focused, selected bool) string {
	pointer := "  "
	border := royalRule.Render("│")
	if focused {
		pointer = royalPointer.Render("› ")
		border = royalGold.Render("│")
	}
	checked := "[ ]"
	if selected {
		checked = "[✓]"
	}
	state := royalGold.Render(fmt.Sprintf("%-11s", "Download"))
	if option.Installed {
		state = royalGreen.Render(fmt.Sprintf("%-11s", "Installed"))
	}
	provider := option.Endpoint.Name
	if provider == "" {
		provider = option.Ref.EndpointID
	}
	providerColumn := royalCyan.Render(fmt.Sprintf("%-10s", provider))
	row := pointer + border + " " + royalText.Render(checked) + " " + providerColumn + " " + state + " " + royalText.Render(option.Ref.ModelID)
	metadata := modelMetadata(option)
	if option.PopularityRank > 0 && len([]rune(option.Ref.ModelID)) >= 24 {
		return row
	}
	return row + "  " + royalMuted.Render(metadata)
}

func groupedModelRows(catalog []setup.ModelOption, start, end, cursor int, selected func(setup.ModelRef) bool) []string {
	if start < 0 {
		start = 0
	}
	if end > len(catalog) {
		end = len(catalog)
	}
	if start >= end {
		return nil
	}
	rows := make([]string, 0, end-start+10)
	installedHeader := false
	popularHeader := false
	currentProvider := ""
	for index := start; index < end; index++ {
		option := catalog[index]
		if option.Installed {
			if !installedHeader {
				rows = append(rows, modelTableHeader())
				installedHeader = true
			}
		} else if option.PopularityRank > 0 {
			if !popularHeader {
				rows = append(rows, "", royalText.Render("Popular downloads"))
				rows = append(rows, styledParagraph("Not sure what to choose? These are widely downloaded compatible models. Popularity is measured separately for each provider.", 88, royalMuted)...)
				popularHeader = true
			}
			provider := modelProviderLabel(option)
			if provider != currentProvider {
				rows = append(rows, "", royalText.Render("Popular on "+provider), modelTableHeader())
				currentProvider = provider
			}
		} else if !installedHeader {
			rows = append(rows, modelTableHeader())
			installedHeader = true
		}
		rows = append(rows, modelOptionRow(option, index == cursor, selected(option.Ref)))
		if option.PopularityRank > 0 && len([]rune(option.Ref.ModelID)) >= 24 {
			rows = append(rows, strings.Repeat(" ", 30)+royalMuted.Render(modelMetadata(option)))
		}
	}
	return rows
}

func modelTableHeader() string {
	return "        " + royalMuted.Render(fmt.Sprintf("%-10s %-11s %s", "Provider", "Status", "Model"))
}

func providerCount(catalog []setup.ModelOption) int {
	return len(providerLabels(catalog))
}

func providerLabels(catalog []setup.ModelOption) []string {
	seen := make(map[string]bool)
	labels := make([]string, 0, 2)
	for _, preferred := range []string{"Ollama", "MLX"} {
		for _, option := range catalog {
			if option.Endpoint.Name == preferred && !seen[preferred] {
				seen[preferred] = true
				labels = append(labels, preferred)
			}
		}
	}
	for _, option := range catalog {
		label := option.Endpoint.Name
		if label == "" {
			label = option.Ref.EndpointID
		}
		if label != "" && !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return labels
}

const modelWindowSize = 8

func modelWindow(total, cursor int) (start, end int) {
	return modelWindowSized(total, cursor, modelWindowSize)
}

func modelWindowForHeight(total, cursor, height int) (start, end int) {
	size := modelWindowSize
	if height >= 46 {
		size = 12
	} else if height > 0 && height < 36 {
		size = height - 28
		if size < 3 {
			size = 3
		}
	}
	return modelWindowSized(total, cursor, size)
}

func modelWindowSized(total, cursor, size int) (start, end int) {
	if total <= size {
		return 0, total
	}
	start = cursor - size/2
	if start < 0 {
		start = 0
	}
	end = start + size
	if end > total {
		end = total
		start = end - size
	}
	return start, end
}

func modelMetadata(option setup.ModelOption) string {
	parts := make([]string, 0, 5)
	if option.ParameterSize != "" {
		parts = append(parts, option.ParameterSize)
	}
	if option.SizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("%.1f GB", float64(option.SizeBytes)/1_000_000_000))
	}
	if option.Quantization != "" {
		parts = append(parts, option.Quantization)
	}
	if option.PopularityRank > 0 {
		parts = append(parts, fmt.Sprintf("#%d popular available", option.PopularityRank))
	}
	if option.PopularityDownloads > 0 {
		label := "downloads"
		if strings.EqualFold(modelProviderLabel(option), "Ollama") {
			label = "pulls"
		}
		parts = append(parts, formatCompactCount(option.PopularityDownloads)+" "+label)
	}
	if len(parts) == 0 {
		return "size unknown"
	}
	return strings.Join(parts, " · ")
}

func formatCompactCount(count int64) string {
	type scale struct {
		value  int64
		suffix string
	}
	for _, candidate := range []scale{{1_000_000_000, "B"}, {1_000_000, "M"}, {1_000, "K"}} {
		if count >= candidate.value {
			value := float64(count) / float64(candidate.value)
			if value == float64(int64(value)) {
				return fmt.Sprintf("%.0f%s", value, candidate.suffix)
			}
			return fmt.Sprintf("%.1f%s", value, candidate.suffix)
		}
	}
	return fmt.Sprintf("%d", count)
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
