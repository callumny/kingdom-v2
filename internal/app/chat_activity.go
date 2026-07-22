package app

import (
	"strings"

	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
)

func (m Model) chatModelActivity() []ui.ChatModelActivity {
	type activity struct {
		endpoint topology.Endpoint
		model    string
		roles    []string
	}
	endpoints := make(map[string]topology.Endpoint, len(m.config.Topology.Endpoints))
	for _, endpoint := range m.config.Topology.Endpoints {
		endpoints[endpoint.ID] = endpoint
	}
	ordered := make([]string, 0, 3)
	byModel := make(map[string]*activity)
	add := func(role string, assignment topology.Assignment) {
		if !assignment.Complete() {
			return
		}
		key := setup.EndpointIdentity(assignment.EndpointID, assignment.Model)
		current := byModel[key]
		if current == nil {
			current = &activity{endpoint: endpoints[assignment.EndpointID], model: assignment.Model}
			byModel[key] = current
			ordered = append(ordered, key)
		}
		current.roles = append(current.roles, role)
	}
	roles := m.config.Topology.Roles
	add("King", roles.King)
	if m.config.CouncilEnabled {
		add("Council", roles.Council)
	}
	add("Workers", roles.Worker)

	result := make([]ui.ChatModelActivity, 0, len(ordered))
	for _, key := range ordered {
		current := byModel[key]
		provider := strings.TrimSpace(current.endpoint.Name)
		if provider == "" {
			provider = current.endpoint.ID
		}
		result = append(result, ui.ChatModelActivity{Provider: provider, Model: current.model, Roles: strings.Join(current.roles, ", "), Status: "Ready"})
	}
	return result
}
