// Package setup contains the Bubble Tea-independent setup workflow.
package setup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

type EndpointStatus string

const (
	Available   EndpointStatus = "available"
	Empty       EndpointStatus = "empty"
	Unavailable EndpointStatus = "unavailable"
)

type EndpointResult struct {
	Endpoint topology.Endpoint
	Models   []discovery.Model
	Err      error
}

// MergeCandidates merges defaults and configured endpoints. Configured IDs override defaults.
func MergeCandidates(defaults, configured []topology.Endpoint) []topology.Endpoint {
	m := map[string]topology.Endpoint{}
	order := []string{}
	for _, e := range defaults {
		if _, ok := m[e.ID]; !ok {
			order = append(order, e.ID)
		}
		m[e.ID] = e
	}
	for _, e := range configured {
		if _, ok := m[e.ID]; !ok {
			order = append(order, e.ID)
		}
		m[e.ID] = e
	}
	out := make([]topology.Endpoint, 0, len(order))
	for _, id := range order {
		out = append(out, m[id])
	}
	return out
}

func StableCustomID(kind topology.EndpointKind, base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err == nil {
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		u.Path = strings.TrimRight(u.Path, "/")
		u.RawQuery = ""
		u.Fragment = ""
		base = u.String()
	} else {
		base = strings.TrimSpace(base)
	}
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + base))
	return "custom-" + string(kind) + "-" + fmt.Sprintf("%x", sum[:8])
}

// CustomEndpointID is the descriptive alias used by callers building forms.
func CustomEndpointID(kind topology.EndpointKind, base string) string {
	return StableCustomID(kind, base)
}

func ValidateCustom(kind topology.EndpointKind, name, base string) (topology.Endpoint, error) {
	e := topology.Endpoint{ID: StableCustomID(kind, base), Name: strings.TrimSpace(name), Kind: kind, BaseURL: strings.TrimRight(strings.TrimSpace(base), "/")}
	if e.Name == "" {
		e.Name = "Custom endpoint"
	}
	if err := e.Validate(); err != nil {
		return topology.Endpoint{}, err
	}
	return e, nil
}

func EndpointIdentity(endpointID, model string) string {
	return endpointID + "\x00" + strings.TrimSpace(model)
}
func DedupeEndpoints(eps []topology.Endpoint) []topology.Endpoint { return MergeCandidates(nil, eps) }

type Draft struct {
	Config          config.Config
	Results         []EndpointResult
	selectedModels  []ModelRef
	selectedOptions map[ModelRef]ModelOption
	providerReady   map[string]bool
	catalog         []ModelOption
}

func NewDraft(existing config.Config, defaults []topology.Endpoint) Draft {
	c := existing
	c.Topology.Endpoints = MergeCandidates(defaults, existing.Topology.Endpoints)
	if c.CouncilSize < 1 {
		c.CouncilSize = 3
	}
	if c.WorkerConcurrency < 1 {
		c.WorkerConcurrency = 4
	}
	return Draft{
		Config:          c,
		selectedModels:  selectedRoleModels(existing.Topology.Roles),
		selectedOptions: make(map[ModelRef]ModelOption),
		providerReady:   make(map[string]bool),
	}
}

func selectedRoleModels(roles topology.Roles) []ModelRef {
	selected := make([]ModelRef, 0, MaxSelectedModels)
	seen := make(map[ModelRef]bool)
	for _, assignment := range []topology.Assignment{roles.King, roles.Worker, roles.Council} {
		ref := ModelRef{EndpointID: assignment.EndpointID, ModelID: strings.TrimSpace(assignment.Model)}
		if !assignment.Complete() || seen[ref] {
			continue
		}
		seen[ref] = true
		selected = append(selected, ref)
	}
	return selected
}
func (d Draft) HasModels() bool {
	for _, r := range d.Results {
		if len(r.Models) > 0 {
			return true
		}
	}
	return false
}
func (d Draft) Ready() bool { return d.Config.IsReady() }
func (d *Draft) ApplyResults(rs []EndpointResult) {
	d.Results = rs
	d.ReplaceCatalog(modelOptionsFromResults(rs))
	if d.providerReady == nil {
		d.providerReady = make(map[string]bool)
	}
	for _, result := range rs {
		if result.Err == nil {
			d.providerReady[result.Endpoint.ID] = true
			if result.Endpoint.Kind == topology.KindOllama {
				d.providerReady[OllamaEndpointID] = true
			}
		}
	}
}
func (d *Draft) AssignKing(a topology.Assignment)   { d.Config.Topology.Roles.King = a }
func (d *Draft) AssignWorker(a topology.Assignment) { d.Config.Topology.Roles.Worker = a }
func (d *Draft) AssignCouncil(a topology.Assignment) {
	d.Config.Topology.Roles.Council = a
	d.Config.CouncilEnabled = true
}
func (d *Draft) SetCouncilEnabled(enabled bool) {
	d.Config.CouncilEnabled = enabled
	if !enabled {
		d.Config.Topology.Roles.Council = topology.Assignment{}
	}
}
func (d Draft) PersistenceEndpoints(previous []topology.Endpoint) []topology.Endpoint {
	all := append([]topology.Endpoint{}, previous...)
	ids := []string{d.Config.Topology.Roles.King.EndpointID, d.Config.Topology.Roles.Worker.EndpointID, d.Config.Topology.Roles.Council.EndpointID}
	for _, e := range d.Config.Topology.Endpoints {
		selected := false
		for _, id := range ids {
			if id != "" && e.ID == id {
				selected = true
			}
		}
		if selected || strings.HasPrefix(e.ID, "custom-") {
			all = append(all, e)
		}
	}
	return DedupeEndpoints(all)
}

type WorkflowState int

const (
	StateProviders WorkflowState = iota
	StateModels
	StateRoles
	StatePerformance
	StateReview
	StateReady
)

type Workflow struct {
	State    WorkflowState
	Draft    Draft
	Previous config.Config
	Err      error
}

func Start(existing config.Config, defaults []topology.Endpoint) *Workflow {
	st := StateProviders
	if existing.IsReady() {
		st = StateReady
	}
	return &Workflow{State: st, Draft: NewDraft(existing, defaults), Previous: existing}
}
func (w *Workflow) Continue() error {
	switch w.State {
	case StateProviders:
		if !w.Draft.Config.Providers.AnyEnabled() {
			return fmt.Errorf("enable at least one provider")
		}
		if err := w.Draft.ValidateEnabledProvidersReady(); err != nil {
			return err
		}
		w.State = StateModels
	case StateModels:
		if len(w.Draft.SelectedModels()) == 0 {
			return fmt.Errorf("select at least one model")
		}
		if err := w.Draft.ApplyRoleSuggestions(); err != nil {
			return err
		}
		w.State = StateRoles
	case StateRoles:
		if !w.Draft.Config.Topology.Roles.King.Complete() || !w.Draft.Config.Topology.Roles.Worker.Complete() {
			return fmt.Errorf("king and worker assignments are required")
		}
		if w.Draft.Config.CouncilEnabled && !w.Draft.Config.Topology.Roles.Council.Complete() {
			return fmt.Errorf("assign a council model or disable the council")
		}
		w.State = StatePerformance
	case StatePerformance:
		if err := config.ValidateOllamaPortPlan(w.Draft.Config); err != nil {
			return err
		}
		w.State = StateReview
	}
	return nil
}
func (w *Workflow) Back() {
	if w.State == StateReview {
		w.State = StatePerformance
	} else if w.State == StatePerformance {
		w.State = StateRoles
	} else if w.State == StateRoles {
		w.State = StateModels
	} else if w.State == StateModels {
		w.State = StateProviders
	}
}
func (w *Workflow) Save(ctx context.Context, save func(config.Config) error) error {
	if w.State != StateReview {
		return fmt.Errorf("not on review")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := save(w.Draft.Config); err != nil {
		w.Err = err
		return err
	}
	w.Err = nil
	w.State = StateReady
	return nil
}
func Status(r EndpointResult) EndpointStatus {
	if r.Err != nil {
		return Unavailable
	}
	if len(r.Models) == 0 {
		return Empty
	}
	return Available
}

func SortResults(rs []EndpointResult) {
	sort.SliceStable(rs, func(i, j int) bool {
		return strings.ToLower(rs[i].Endpoint.Name) < strings.ToLower(rs[j].Endpoint.Name)
	})
}

// GenerationGate ensures an older asynchronous discovery cannot publish over a newer scan.
type GenerationGate struct {
	mu         sync.Mutex
	generation uint64
	cancel     context.CancelFunc
	canceled   uint64
}

func (g *GenerationGate) Begin(parent context.Context) (uint64, context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel != nil {
		g.cancel()
	}
	ctx, c := context.WithCancel(parent)
	g.cancel = c
	g.generation++
	g.canceled = 0
	return g.generation, ctx
}
func (g *GenerationGate) Accept(gen uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return gen == g.generation && g.canceled != gen
}
func (g *GenerationGate) Cancel() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel != nil {
		g.cancel()
	}
	g.cancel = nil
	g.generation++
	g.canceled = g.generation
}

func ClampCouncilSize(v int) int {
	if v < 1 {
		return 1
	}
	if v > 9 {
		return 9
	}
	return v
}
func ClampWorkerConcurrency(v int) int {
	if v < 1 {
		return 1
	}
	if v > 32 {
		return 32
	}
	return v
}
