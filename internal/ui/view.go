package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

var titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

type Presentation struct {
	ModelIndex, Role, PerfFocus  int
	Form                         *CustomEndpointForm
	PreviousEndpoints            []topology.Endpoint
	FormActive, Scanning, Saving bool
}

// ViewWithPresentation renders the complete presentation before applying the
// terminal height limit.
func ViewWithPresentation(width, height int, setupRequired bool, wf *setup.Workflow, p Presentation) tea.View {
	lines := []string{"Kingdom"}
	if !setupRequired || (wf != nil && wf.State == setup.StateReady) {
		lines = append(lines, "", "Configuration ready.", "", "Press s to reopen setup, q to quit.")
	} else {
		lines = append(lines, "", "Setup required.")
		if wf == nil {
			lines = append(lines, "", "Performance (j/k select, h/l adjust, Enter continue):")
			first, second := "> ", "  "
			if p.PerfFocus == 1 {
				first, second = "  ", "> "
			}
			lines = append(lines, first+"Council size", second+"Worker concurrency")
		}
		if wf == nil {
			goto done
		}
		switch wf.State {
		case setup.StateDiscovery:
			if p.Scanning {
				lines = append(lines, "", "Scanning...")
			} else {
				lines = append(lines, "", "Scan complete")
			}
			lines = append(lines, "Enter: continue   r: rescan   q: quit")
			for i, r := range wf.Draft.Results {
				if i >= 200 {
					lines = append(lines, "...")
					break
				}
				lines = append(lines, r.Endpoint.Name+": "+string(setup.Status(r)))
			}
		case setup.StateRoles:
			label := map[int]string{0: "King", 1: "Worker", 2: "Council"}[p.Role]
			if label == "" {
				label = "King"
			}
			lines = append(lines, "", "Assign role: "+label, "1: King   2: Worker   3: Council   0: Council uses King")
			idx := 0
			for _, r := range wf.Draft.Results {
				for _, m := range r.Models {
					marker := "  "
					if idx == p.ModelIndex {
						marker = "> "
					}
					lines = append(lines, marker+r.Endpoint.Name+" ("+r.Endpoint.ID+") / "+m.ID)
					idx++
				}
			}
			r := wf.Draft.Config.Topology.Roles
			council := fmt.Sprintf("%s/%s", r.Council.EndpointID, r.Council.Model)
			if wf.Draft.CouncilUseKing {
				council = "uses King"
			}
			lines = append(lines, fmt.Sprintf("King: %s/%s  Worker: %s/%s  Council: %s", r.King.EndpointID, r.King.Model, r.Worker.EndpointID, r.Worker.Model, council), "Arrows: select   Enter: assign   n: next   Esc: back")
		case setup.StatePerformance:
			lines = append(lines, "", "Performance (j/k select, h/l adjust, Enter continue):")
			first, second := "> ", "  "
			if p.PerfFocus == 1 {
				first, second = "  ", "> "
			}
			lines = append(lines, fmt.Sprintf("%sCouncil size: %d", first, wf.Draft.Config.CouncilSize), fmt.Sprintf("%sWorker concurrency: %d", second, wf.Draft.Config.WorkerConcurrency))
		case setup.StateReview:
			lines = append(lines, "", "Review assignments.")
			r := wf.Draft.Config.Topology.Roles
			council := fmt.Sprintf("%s/%s", r.Council.EndpointID, r.Council.Model)
			if wf.Draft.CouncilUseKing {
				council = "uses King"
			}
			lines = append(lines,
				fmt.Sprintf("King: %s/%s", r.King.EndpointID, r.King.Model),
				fmt.Sprintf("Worker: %s/%s", r.Worker.EndpointID, r.Worker.Model),
				"Council: "+council,
				fmt.Sprintf("Council size: %d", wf.Draft.Config.CouncilSize),
				fmt.Sprintf("Worker concurrency: %d", wf.Draft.Config.WorkerConcurrency),
			)
			for _, e := range wf.Draft.PersistenceEndpoints(p.PreviousEndpoints) {
				lines = append(lines, fmt.Sprintf("Endpoint: %s (%s)", e.Name, e.BaseURL))
			}
			if p.Saving {
				lines = append(lines, "Saving...")
			} else {
				lines = append(lines, "Enter: save   Esc: back")
			}
			if wf.Err != nil {
				lines = append(lines, "Save error: "+wf.Err.Error())
			}
		}
	}
done:
	if p.FormActive && p.Form != nil {
		lines = append(lines, "", p.Form.View())
	}
	if width > 0 && height > 0 {
		lines = append(lines, fmt.Sprintf("%d×%d", width, height))
	}
	content := titleStyle.Render(strings.Join(lines, "\n"))
	if height > 0 {
		ls := strings.Split(content, "\n")
		if len(ls) > height {
			content = strings.Join(ls[:height], "\n")
		}
	}
	return tea.NewView(content)
}

func View(width, height int, setupRequired bool, wf ...*setup.Workflow) tea.View {
	var w *setup.Workflow
	if len(wf) > 0 {
		w = wf[0]
	}
	return ViewWithPresentation(width, height, setupRequired, w, Presentation{})
}
