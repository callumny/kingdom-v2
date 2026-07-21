package ui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/setup"
)

func performanceSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	cfg := wf.Draft.Config
	body := []string{
		royalBrand.Render("Advanced performance"),
		"",
		royalText.Render("Choose how much work Kingdom may run at once."),
		royalMuted.Render("Higher settings can be faster, but use more memory and processing power."),
		"",
		performanceRow(p.PerfFocus == 0, "Council members", fmt.Sprintf("%d", cfg.CouncilSize)),
		royalMuted.Render("  Independent reviewers used when the council is enabled."),
		performanceRow(p.PerfFocus == 1, "Concurrent workers", fmt.Sprintf("%d", cfg.WorkerConcurrency)),
		royalMuted.Render("  Tasks Kingdom may work on at the same time."),
	}

	footer := "↑↓ Move   •   ←→ Adjust   •   Enter Continue   •   Esc Back"
	if config.UsesManagedOllama(cfg) {
		dedicated := cfg.Providers.Ollama.PortMode == config.OllamaDedicatedPorts
		state := royalMuted.Render("OFF")
		if dedicated {
			state = royalGreen.Render("ON") + "  " + royalPending.Render("Recommended")
		}
		body = append(body,
			"",
			performanceRow(p.PerfFocus == 2, "Separate Ollama servers", state),
		)
		if dedicated {
			body = append(body, royalMuted.Render("  Reduces cross-model contention; each selected model gets a port."))
		} else {
			body = append(body, royalMuted.Render("  One shared server can use less memory, but may create model contention."))
		}
		body = append(body, ollamaRouteLines(cfg)...)
		body = append(body, royalMuted.Render("  MLX is unaffected because it already runs one model per server."))
		footer = "↑↓ Move   •   ←→ Adjust   •   Space Toggle   •   Enter Continue   •   Esc Back"
	}
	if wf.Err != nil {
		body = append(body, "", royalRed.Render(wf.Err.Error()))
	}
	return body, royalMuted.Render(footer)
}

func performanceRow(focused bool, label, value string) string {
	prefix := "  "
	if focused {
		prefix = "> "
	}
	return fmt.Sprintf("%s%-28s %s", prefix, label, value)
}

func reviewOllamaSummary(cfg config.Config) []string {
	if !config.UsesManagedOllama(cfg) {
		return nil
	}
	mode := "shared"
	if cfg.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
		mode = "separate"
	}
	lines := []string{"Ollama servers: " + mode}
	return append(lines, ollamaRouteLines(cfg)...)
}

func ollamaRouteLines(cfg config.Config) []string {
	plan, err := config.BuildRuntimePlan(cfg)
	if err != nil {
		return []string{royalRed.Render("  Port plan unavailable: " + err.Error())}
	}
	if len(plan.OllamaRoutes) == 0 {
		return nil
	}
	if cfg.Providers.Ollama.PortMode == config.OllamaSharedPort {
		models := make([]string, 0, len(plan.OllamaRoutes))
		for _, route := range plan.OllamaRoutes {
			models = append(models, route.Model)
		}
		return []string{royalCyan.Render("  " + strings.Join(models, " + ") + " → " + routeAddress(plan.OllamaRoutes[0].Endpoint.BaseURL))}
	}
	lines := make([]string, 0, len(plan.OllamaRoutes))
	for _, route := range plan.OllamaRoutes {
		lines = append(lines, royalCyan.Render("  "+route.Model+" → "+routeAddress(route.Endpoint.BaseURL)))
	}
	return lines
}

func routeAddress(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
}
