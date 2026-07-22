package setup

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

var parameterHintPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)b(?:[^a-z0-9]|$)`)

func inferredParameterSize(modelID string) string {
	match := parameterHintPattern.FindStringSubmatch(modelID)
	if len(match) != 2 {
		return ""
	}
	return strings.ToUpper(match[1]) + "B"
}

// ApplyRoleSuggestions fills invalid or incomplete roles from the selected
// model pool. Existing valid choices are preserved when the user revisits the
// Models screen without changing that pool.
func (d *Draft) ApplyRoleSuggestions() error {
	selected := d.SelectedModels()
	if len(selected) == 0 {
		return fmt.Errorf("select at least one model")
	}
	if d.rolesUseSelectedModels(selected) {
		return nil
	}

	ordered := append([]ModelOption(nil), selected...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return modelScale(ordered[i]) < modelScale(ordered[j])
	})
	smallest := ordered[0].Ref.Assignment()
	largest := ordered[len(ordered)-1].Ref.Assignment()
	d.AssignKing(largest)
	d.AssignWorker(smallest)
	if len(ordered) < 3 {
		d.SetCouncilEnabled(false)
	} else {
		d.AssignCouncil(ordered[len(ordered)/2].Ref.Assignment())
	}
	return nil
}

func (d Draft) rolesUseSelectedModels(selected []ModelOption) bool {
	available := make(map[ModelRef]bool, len(selected))
	for _, option := range selected {
		available[option.Ref] = true
	}
	roles := d.Config.Topology.Roles
	if !available[assignmentRef(roles.King)] || !available[assignmentRef(roles.Worker)] {
		return false
	}
	return !d.Config.CouncilEnabled || available[assignmentRef(roles.Council)]
}

func assignmentRef(assignment topology.Assignment) ModelRef {
	return ModelRef{EndpointID: assignment.EndpointID, ModelID: strings.TrimSpace(assignment.Model)}
}

func modelScale(option ModelOption) float64 {
	if parsed, ok := parseParameterSize(option.ParameterSize); ok {
		return parsed
	}
	if match := parameterHintPattern.FindStringSubmatch(option.Ref.ModelID); len(match) == 2 {
		if parsed, err := strconv.ParseFloat(match[1], 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	if option.SizeBytes > 0 {
		return float64(option.SizeBytes) / 1_000_000_000
	}
	return 0
}

func parseParameterSize(value string) (float64, bool) {
	value = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	if value == "" {
		return 0, false
	}
	multiplier := 1.0
	switch value[len(value)-1] {
	case 'K':
		multiplier = 0.000001
	case 'M':
		multiplier = 0.001
	case 'B':
		multiplier = 1
	case 'T':
		multiplier = 1000
	default:
		return 0, false
	}
	number, err := strconv.ParseFloat(value[:len(value)-1], 64)
	if err != nil || number <= 0 {
		return 0, false
	}
	return number * multiplier, true
}
