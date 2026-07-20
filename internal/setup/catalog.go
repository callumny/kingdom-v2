package setup

import (
	"fmt"
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

const MaxSelectedModels = 3

// ModelRef identifies a model within one provider endpoint. Model IDs are not
// globally unique, so both fields are required.
type ModelRef struct {
	EndpointID string
	ModelID    string
}

func (r ModelRef) Assignment() topology.Assignment {
	return topology.Assignment{EndpointID: r.EndpointID, Model: r.ModelID}
}

// ModelOption is the provider-neutral representation used by setup screens.
// Endpoint keeps the routing information; the remaining fields are optional
// presentation metadata supplied by discovery.
type ModelOption struct {
	Ref           ModelRef
	Endpoint      topology.Endpoint
	SizeBytes     int64
	Family        string
	ParameterSize string
	Quantization  string
}

// Catalog flattens provider discovery results into stable, unique choices.
func (d Draft) Catalog() []ModelOption {
	seen := make(map[ModelRef]bool)
	options := make([]ModelOption, 0)
	for _, result := range d.Results {
		endpointID := strings.TrimSpace(result.Endpoint.ID)
		if endpointID == "" {
			continue
		}
		for _, model := range result.Models {
			ref := ModelRef{EndpointID: endpointID, ModelID: strings.TrimSpace(model.ID)}
			if ref.ModelID == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			options = append(options, ModelOption{
				Ref:           ref,
				Endpoint:      result.Endpoint,
				SizeBytes:     model.SizeBytes,
				Family:        model.Family,
				ParameterSize: model.ParameterSize,
				Quantization:  model.Quantization,
			})
		}
	}
	return options
}

// ToggleModel adds or removes one catalogue choice from the transient setup
// selection. Selections are intentionally not part of persisted config.
func (d *Draft) ToggleModel(ref ModelRef) error {
	for index, selected := range d.selectedModels {
		if selected == ref {
			d.selectedModels = append(d.selectedModels[:index], d.selectedModels[index+1:]...)
			return nil
		}
	}
	if !d.catalogContains(ref) {
		return fmt.Errorf("model %q is not available from endpoint %q", ref.ModelID, ref.EndpointID)
	}
	if len(d.selectedModels) >= MaxSelectedModels {
		return fmt.Errorf("select up to %d models", MaxSelectedModels)
	}
	d.selectedModels = append(d.selectedModels, ref)
	return nil
}

func (d Draft) IsModelSelected(ref ModelRef) bool {
	for _, selected := range d.selectedModels {
		if selected == ref {
			return true
		}
	}
	return false
}

// SelectedModels returns selected choices in the order the user chose them.
func (d Draft) SelectedModels() []ModelOption {
	byRef := make(map[ModelRef]ModelOption)
	for _, option := range d.Catalog() {
		byRef[option.Ref] = option
	}
	selected := make([]ModelOption, 0, len(d.selectedModels))
	for _, ref := range d.selectedModels {
		if option, exists := byRef[ref]; exists {
			selected = append(selected, option)
		}
	}
	return selected
}

// ReconcileModelSelection removes choices that disappeared on a rescan.
func (d *Draft) ReconcileModelSelection() []ModelRef {
	available := make(map[ModelRef]bool)
	for _, option := range d.Catalog() {
		available[option.Ref] = true
	}
	kept := d.selectedModels[:0]
	removed := make([]ModelRef, 0)
	for _, ref := range d.selectedModels {
		if available[ref] {
			kept = append(kept, ref)
		} else {
			removed = append(removed, ref)
		}
	}
	d.selectedModels = kept
	return removed
}

func (d Draft) catalogContains(ref ModelRef) bool {
	for _, option := range d.Catalog() {
		if option.Ref == ref {
			return true
		}
	}
	return false
}
