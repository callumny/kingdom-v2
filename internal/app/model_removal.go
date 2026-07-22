package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
)

func (m Model) beginModelRemoval() (tea.Model, tea.Cmd) {
	target := m.modelRemoveTarget
	if !target.Installed || target.Ref == (setup.ModelRef{}) {
		m.modelRemoveConfirming = false
		m.workflow.Err = fmt.Errorf("select an installed model to uninstall")
		return m, nil
	}
	if m.modelRemover == nil {
		m.modelRemoveConfirming = false
		m.workflow.Err = fmt.Errorf("model uninstaller is unavailable")
		return m, nil
	}
	kind, ok := localKindForEndpoint(target.Ref.EndpointID)
	if !ok {
		m.modelRemoveConfirming = false
		m.workflow.Err = fmt.Errorf("unsupported model provider %q", target.Ref.EndpointID)
		return m, nil
	}

	m.modelRemoveConfirming = false
	m.modelRemoveActive = true
	m.modelRemoveNotice = ""
	m.workflow.Err = nil
	m.modelRemoveGen++
	generation := m.modelRemoveGen
	ctx, cancel := context.WithCancel(context.Background())
	m.modelRemoveCancel = cancel
	remover := m.modelRemover
	request := localmodels.RemoveRequest{Kind: kind, Model: target.Ref.ModelID, BaseURL: target.Endpoint.BaseURL}
	return m, func() tea.Msg {
		return modelRemoveMsg{generation: generation, ref: target.Ref, err: remover.Remove(ctx, request)}
	}
}
