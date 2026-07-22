package ui

import (
	"fmt"

	"charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type Presentation struct {
	ModelIndex, ModelCursor, Role, ProviderCursor, PerfFocus int
	Form                                                     *CustomEndpointForm
	PreviousEndpoints                                        []topology.Endpoint
	FormActive, Scanning, ModelInventoryLoading, Saving      bool
	ProviderConfirming, ProviderInstalling                   bool
	ProviderNotice                                           string
	ProviderProgress                                         localmodels.InstallProgress
	ModelDownloadProgress                                    localmodels.DownloadProgress
	ModelQuery, ModelSearchWarning                           string
	ModelSearchActive, ModelSearching                        bool
	ModelDownloadConfirming                                  bool
	ModelDownloadActive                                      bool
	ModelDownloadError                                       string
	BenchmarkActive, WizardBusy, WizardReady, WizardApplying bool
	BenchmarkModel, BenchmarkPhase, WizardModel, WizardInput string
	BenchmarkResults                                         []WizardBenchmarkRow
	WizardMessages                                           []string
}

// ViewWithPresentation renders the complete presentation before applying the
// terminal height limit.
func ViewWithPresentation(width, height int, setupRequired bool, wf *setup.Workflow, p Presentation) tea.View {
	var body []string
	progress := ""
	footer := royalMuted.Render("q Quit")
	if !setupRequired || (wf != nil && wf.State == setup.StateReady) {
		body = []string{royalBrand.Render("Configuration ready"), "", royalText.Render("Kingdom is ready to use your local models.")}
		footer = royalMuted.Render("s Reopen setup   •   q Quit")
	} else {
		if wf == nil {
			body = append(body, royalBrand.Render("Setup required"), "", "Performance")
			first, second := "> ", "  "
			if p.PerfFocus == 1 {
				first, second = "  ", "> "
			}
			body = append(body, first+"Council size", second+"Worker concurrency")
		}
		if wf == nil {
			return tea.NewView(renderRoyalShell(width, height, progress, body, footer))
		}
		switch wf.State {
		case setup.StateProviders:
			progress = setupProgress(1)
			body, footer = providersSetupView(wf, p)
		case setup.StateModels:
			progress = setupProgress(2)
			body, footer = modelsSetupView(wf, p)
		case setup.StateBenchmark:
			progress = setupProgress(3)
			body, footer = benchmarkSetupView(wf, p)
		case setup.StateWizard:
			progress = setupProgress(3)
			body, footer = wizardSetupView(wf, p)
		case setup.StateRoles:
			progress = setupProgress(3)
			body, footer = rolesSetupView(wf, p)
		case setup.StatePerformance:
			progress = setupProgress(4)
			body, footer = performanceSetupView(wf, p)
		case setup.StateReview:
			progress = setupProgress(4)
			body = append(body, royalBrand.Render("Review your setup"), "")
			r := wf.Draft.Config.Topology.Roles
			council := fmt.Sprintf("%s/%s", r.Council.EndpointID, r.Council.Model)
			if !wf.Draft.Config.CouncilEnabled {
				council = "disabled"
			}
			body = append(body,
				fmt.Sprintf("King: %s/%s", r.King.EndpointID, r.King.Model),
				fmt.Sprintf("Worker: %s/%s", r.Worker.EndpointID, r.Worker.Model),
				"Council: "+council,
				fmt.Sprintf("Council size: %d", wf.Draft.Config.CouncilSize),
				fmt.Sprintf("Worker concurrency: %d", wf.Draft.Config.WorkerConcurrency),
			)
			body = append(body, reviewOllamaSummary(wf.Draft.Config)...)
			for _, e := range wf.Draft.PersistenceEndpoints(p.PreviousEndpoints) {
				body = append(body, fmt.Sprintf("Endpoint: %s (%s)", e.Name, e.BaseURL))
			}
			if p.Saving {
				body = append(body, "", royalCyan.Render("Saving…"))
			} else {
				footer = royalMuted.Render("Enter Save setup   •   Esc Back")
			}
			if wf.Err != nil {
				body = append(body, "", royalRed.Render("Save error: "+wf.Err.Error()))
			}
		}
	}
	if p.FormActive && p.Form != nil {
		body = append(body, "", p.Form.View())
	}
	return tea.NewView(renderRoyalShell(width, height, progress, body, footer))
}

func View(width, height int, setupRequired bool, wf ...*setup.Workflow) tea.View {
	var w *setup.Workflow
	if len(wf) > 0 {
		w = wf[0]
	}
	return ViewWithPresentation(width, height, setupRequired, w, Presentation{})
}
