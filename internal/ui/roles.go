package ui

import (
	"fmt"

	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

func rolesSetupView(wf *setup.Workflow, p Presentation) ([]string, string) {
	roleName := map[int]string{0: "King", 1: "Worker", 2: "Council"}[p.Role]
	if roleName == "" {
		roleName = "King"
	}
	body := []string{royalBrand.Render("Assign models to roles"), ""}
	body = append(body,
		royalText.Render("King")+royalMuted.Render(" — plans and coordinates; a larger model is usually a good fit."),
		royalText.Render("Worker")+royalMuted.Render(" — handles focused tasks; a smaller model is usually faster."),
		royalText.Render("Council")+royalMuted.Render(" — provides independent review; a different model adds perspective."),
		"",
		royalGold.Render("Editing: "+roleName),
		royalMuted.Render("Press 1 for King, 2 for Worker, 3 for Council, or 0 for Council to use King."),
		"",
	)
	for index, option := range wf.Draft.SelectedModels() {
		marker := "  "
		if index == p.ModelIndex {
			marker = royalPointer.Render("› ")
		}
		body = append(body, marker+royalText.Render(option.Endpoint.Name+" / "+option.Ref.ModelID)+"  "+royalMuted.Render(modelMetadata(option)))
	}
	body = append(body, "", royalGold.Render("Current assignments"))
	roles := wf.Draft.Config.Topology.Roles
	body = append(body,
		"King:    "+assignmentLabel(roles.King, wf),
		"Worker:  "+assignmentLabel(roles.Worker, wf),
	)
	if wf.Draft.CouncilUseKing {
		body = append(body, "Council: "+royalMuted.Render("uses King"))
	} else {
		body = append(body, "Council: "+assignmentLabel(roles.Council, wf))
	}
	return body, royalMuted.Render("↑↓ Move   •   Enter Assign   •   n Continue   •   Esc Back")
}

func assignmentLabel(assignment topology.Assignment, wf *setup.Workflow) string {
	if !assignment.Complete() {
		return royalMuted.Render("not assigned")
	}
	provider := assignment.EndpointID
	for _, option := range wf.Draft.Catalog() {
		if option.Ref == (setup.ModelRef{EndpointID: assignment.EndpointID, ModelID: assignment.Model}) {
			provider = option.Endpoint.Name
			break
		}
	}
	return fmt.Sprintf("%s / %s", provider, assignment.Model)
}
