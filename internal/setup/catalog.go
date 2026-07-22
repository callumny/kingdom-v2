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
	Installed     bool
	SizeBytes     int64
	Family        string
	ParameterSize string
	Quantization  string
}

// Catalog returns the choices supplied by the application boundary. Keeping
// inventory separate from provider health lets cached MLX models appear even
// when no MLX model server is currently running.
func (d Draft) Catalog() []ModelOption {
	if d.catalog != nil {
		return append([]ModelOption(nil), d.catalog...)
	}
	return modelOptionsFromResults(d.Results)
}

// ReplaceCatalog atomically replaces the transient choices shown during setup.
// Model identity includes the endpoint because names may overlap across
// providers.
func (d *Draft) ReplaceCatalog(options []ModelOption) {
	if d.selectedOptions == nil {
		d.selectedOptions = make(map[ModelRef]ModelOption)
	}
	seen := make(map[ModelRef]bool)
	normalized := make([]ModelOption, 0, len(options))
	for _, option := range options {
		option.Ref.EndpointID = strings.TrimSpace(option.Ref.EndpointID)
		option.Ref.ModelID = strings.TrimSpace(option.Ref.ModelID)
		if option.Ref.EndpointID == "" || option.Ref.ModelID == "" || seen[option.Ref] {
			continue
		}
		if option.ParameterSize == "" {
			option.ParameterSize = inferredParameterSize(option.Ref.ModelID)
		}
		seen[option.Ref] = true
		normalized = append(normalized, option)
		if d.IsModelSelected(option.Ref) {
			d.selectedOptions[option.Ref] = option
		}
	}
	d.catalog = normalized
}

func modelOptionsFromResults(results []EndpointResult) []ModelOption {
	options := make([]ModelOption, 0)
	for _, result := range results {
		for _, model := range result.Models {
			options = append(options, ModelOption{
				Ref:           ModelRef{EndpointID: result.Endpoint.ID, ModelID: model.ID},
				Endpoint:      result.Endpoint,
				Installed:     true,
				SizeBytes:     model.SizeBytes,
				Family:        model.Family,
				ParameterSize: model.ParameterSize,
				Quantization:  model.Quantization,
			})
		}
	}
	draft := Draft{}
	draft.ReplaceCatalog(options)
	return draft.catalog
}

// ToggleModel adds or removes one catalogue choice from the transient setup
// selection. Selections are intentionally not part of persisted config.
func (d *Draft) ToggleModel(ref ModelRef) error {
	for index, selected := range d.selectedModels {
		if selected == ref {
			d.selectedModels = append(d.selectedModels[:index], d.selectedModels[index+1:]...)
			delete(d.selectedOptions, ref)
			return nil
		}
	}
	if !d.catalogContains(ref) {
		return fmt.Errorf("model %q is not available from endpoint %q", ref.ModelID, ref.EndpointID)
	}
	if len(d.selectedModels) >= MaxSelectedModels {
		return fmt.Errorf("select up to %d models", MaxSelectedModels)
	}
	for _, option := range d.Catalog() {
		if option.Ref == ref {
			if d.selectedOptions == nil {
				d.selectedOptions = make(map[ModelRef]ModelOption)
			}
			d.selectedOptions[ref] = option
			break
		}
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
	for ref, option := range d.selectedOptions {
		byRef[ref] = option
	}
	selected := make([]ModelOption, 0, len(d.selectedModels))
	for _, ref := range d.selectedModels {
		if option, exists := byRef[ref]; exists {
			selected = append(selected, option)
		}
	}
	return selected
}

// PendingDownloads returns selected online models in selection order.
func (d Draft) PendingDownloads() []ModelOption {
	pending := make([]ModelOption, 0)
	for _, option := range d.SelectedModels() {
		if !option.Installed {
			pending = append(pending, option)
		}
	}
	return pending
}

func (d *Draft) MarkModelInstalled(ref ModelRef) {
	if option, exists := d.selectedOptions[ref]; exists {
		option.Installed = true
		d.selectedOptions[ref] = option
	}
	for index := range d.catalog {
		if d.catalog[index].Ref == ref {
			d.catalog[index].Installed = true
		}
	}
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
			delete(d.selectedOptions, ref)
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
