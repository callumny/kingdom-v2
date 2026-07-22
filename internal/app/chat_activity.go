package app

import (
	"fmt"
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
		metric := m.modelMetrics[modelMetricKey(current.endpoint.Kind, current.model)]
		speed := 0.0
		if metric.completionTokens > 0 && metric.generationDuration > 0 {
			speed = float64(metric.completionTokens) / metric.generationDuration.Seconds()
		}
		result = append(result, ui.ChatModelActivity{
			Provider:        provider,
			Model:           current.model,
			Roles:           strings.Join(current.roles, ", "),
			Status:          m.modelStatus(current.roles),
			TokensPerSecond: speed,
		})
	}
	return result
}

func modelMetricKey(kind topology.EndpointKind, model string) string {
	return fmt.Sprintf("%s\x00%s", kind, model)
}

func (m Model) modelStatus(roles []string) string {
	progress := strings.ToLower(m.progress)
	for _, role := range roles {
		switch role {
		case "King":
			if strings.Contains(progress, "king") {
				return "Thinking"
			}
		case "Workers":
			if strings.Contains(progress, "worker") {
				return "Working"
			}
		case "Council":
			if strings.Contains(progress, "council") {
				return "Reviewing"
			}
		}
	}
	if strings.Contains(progress, "starting local model") {
		return "Starting"
	}
	return "Ready"
}
