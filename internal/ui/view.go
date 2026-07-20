package ui

import (
	"fmt"

	"charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type Presentation struct {
	ModelIndex, Role, ProviderCursor, PerfFocus int
	Form                                        *CustomEndpointForm
	PreviousEndpoints                           []topology.Endpoint
	SelectedProviders                           map[string]bool
	FormActive, Scanning, Saving                bool
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
		case setup.StateWelcome:
			body, footer = welcomeSetupView(wf, p)
		case setup.StateProviders:
			progress = setupProgress(1)
			body, footer = providersSetupView(wf, p)
		case setup.StateRoles:
			progress = setupProgress(3)
			label := map[int]string{0: "King", 1: "Worker", 2: "Council"}[p.Role]
			if label == "" {
				label = "King"
			}
			body = append(body, royalBrand.Render("Assign role: "+label), "", "1: King   2: Worker   3: Council   0: Council uses King")
			idx := 0
			for _, r := range wf.Draft.Results {
				if !providerIsSelected(p.SelectedProviders, r.Endpoint.ID) {
					continue
				}
				for _, m := range r.Models {
					marker := "  "
					if idx == p.ModelIndex {
						marker = "> "
					}
					body = append(body, marker+r.Endpoint.Name+" ("+r.Endpoint.ID+") / "+m.ID)
					idx++
				}
			}
			r := wf.Draft.Config.Topology.Roles
			council := fmt.Sprintf("%s/%s", r.Council.EndpointID, r.Council.Model)
			if wf.Draft.CouncilUseKing {
				council = "uses King"
			}
			body = append(body, "", fmt.Sprintf("King: %s/%s  Worker: %s/%s  Council: %s", r.King.EndpointID, r.King.Model, r.Worker.EndpointID, r.Worker.Model, council))
			footer = royalMuted.Render("↑↓ Move   •   Enter Assign   •   n Continue   •   Esc Back")
		case setup.StatePerformance:
			progress = setupProgress(4)
			body = append(body, royalBrand.Render("Advanced performance"), "", "Tune for your hardware.")
			first, second := "> ", "  "
			if p.PerfFocus == 1 {
				first, second = "  ", "> "
			}
			body = append(body, "", fmt.Sprintf("%sCouncil size: %d", first, wf.Draft.Config.CouncilSize), fmt.Sprintf("%sWorker concurrency: %d", second, wf.Draft.Config.WorkerConcurrency))
			footer = royalMuted.Render("↑↓ Move   •   ←→ Adjust   •   Enter Continue   •   Esc Back")
		case setup.StateReview:
			progress = setupProgress(4)
			body = append(body, royalBrand.Render("Review your setup"), "")
			r := wf.Draft.Config.Topology.Roles
			council := fmt.Sprintf("%s/%s", r.Council.EndpointID, r.Council.Model)
			if wf.Draft.CouncilUseKing {
				council = "uses King"
			}
			body = append(body,
				fmt.Sprintf("King: %s/%s", r.King.EndpointID, r.King.Model),
				fmt.Sprintf("Worker: %s/%s", r.Worker.EndpointID, r.Worker.Model),
				"Council: "+council,
				fmt.Sprintf("Council size: %d", wf.Draft.Config.CouncilSize),
				fmt.Sprintf("Worker concurrency: %d", wf.Draft.Config.WorkerConcurrency),
			)
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
