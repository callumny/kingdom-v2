package topology

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// EndpointKind identifies the endpoint protocol compatibility.
type EndpointKind string

const (
	// KindOllama identifies an Ollama endpoint.
	KindOllama EndpointKind = "ollama"
	// KindOpenAICompatible identifies an OpenAI-compatible endpoint.
	KindOpenAICompatible EndpointKind = "openai-compatible"
)

// Endpoint describes a local model service endpoint.
type Endpoint struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Kind    EndpointKind `json:"kind"`
	BaseURL string       `json:"base_url"`
}

// Assignment binds a role to an endpoint and model.
type Assignment struct {
	EndpointID string `json:"endpoint_id"`
	Model      string `json:"model"`
}

// Roles contains assignments for each application role.
type Roles struct {
	King    Assignment `json:"king"`
	Worker  Assignment `json:"worker"`
	Council Assignment `json:"council"`
}

// Topology defines endpoints and role assignments.
type Topology struct {
	Endpoints []Endpoint `json:"endpoints"`
	Roles     Roles      `json:"roles"`
}

// Default returns an empty topology.
func Default() Topology { return Topology{} }

// Validate checks endpoint and assignment structure without probing connectivity.
func (e Endpoint) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("endpoint id is required")
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("endpoint %q name is required", e.ID)
	}
	if e.Kind != KindOllama && e.Kind != KindOpenAICompatible {
		return fmt.Errorf("endpoint %q has unknown kind", e.ID)
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("endpoint %q has invalid base URL", e.ID)
	}
	if u.ForceQuery {
		return fmt.Errorf("endpoint %q has invalid base URL", e.ID)
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("endpoint %q has invalid base URL", e.ID)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("endpoint %q has invalid base URL", e.ID)
		}
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	lowerHost := strings.ToLower(host)
	allowed := strings.EqualFold(host, "localhost") || (strings.HasSuffix(lowerHost, ".local") && len(host) > len(".local"))
	if ip != nil {
		allowed = ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	if !allowed {
		return fmt.Errorf("endpoint %q base URL host is not local/private", e.ID)
	}
	return nil
}

// Empty reports whether no assignment fields are set.
func (a Assignment) Empty() bool { return a.EndpointID == "" && strings.TrimSpace(a.Model) == "" }

// Complete reports whether both assignment fields are populated.
func (a Assignment) Complete() bool { return a.EndpointID != "" && strings.TrimSpace(a.Model) != "" }

// Validate checks topology structure and references.
func (t Topology) Validate() error {
	seen := map[string]bool{}
	for _, e := range t.Endpoints {
		if err := e.Validate(); err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("duplicate endpoint id %q", e.ID)
		}
		seen[e.ID] = true
	}
	for _, item := range []struct {
		name string
		a    Assignment
	}{{"king", t.Roles.King}, {"worker", t.Roles.Worker}, {"council", t.Roles.Council}} {
		name, a := item.name, item.a
		if a.Empty() {
			continue
		}
		if !a.Complete() {
			return fmt.Errorf("%s assignment incomplete", name)
		}
		if !seen[a.EndpointID] {
			return fmt.Errorf("%s assignment references unknown endpoint %q", name, a.EndpointID)
		}
	}
	return nil
}

// IsReady reports whether required assignments are complete and valid.
func (t Topology) IsReady() bool {
	return t.Roles.King.Complete() && t.Roles.Worker.Complete() && t.Validate() == nil
}

// EffectiveCouncil returns the council assignment, falling back to king.
func (t Topology) EffectiveCouncil() *Assignment {
	if !t.Roles.Council.Empty() {
		a := t.Roles.Council
		return &a
	}
	if !t.Roles.King.Empty() {
		a := t.Roles.King
		return &a
	}
	return nil
}
