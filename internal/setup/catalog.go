package setup

import (
	"strings"

	"github.com/callumny/kingdom/internal/topology"
)

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
