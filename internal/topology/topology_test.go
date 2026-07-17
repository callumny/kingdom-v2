package topology

import (
	"strings"
	"testing"
)

func TestDefaultTopologyNotReadyAndCouncilFallback(t *testing.T) {
	topo := Default()
	if topo.IsReady() {
		t.Fatal("default topology should not be ready")
	}
	if topo.EffectiveCouncil() != nil {
		t.Fatal("empty council should have no effective assignment")
	}
	e := Endpoint{ID: "local", Name: "Local", Kind: KindOllama, BaseURL: "http://localhost:11434"}
	topo.Endpoints = []Endpoint{e}
	topo.Roles.King = Assignment{EndpointID: "local", Model: "king"}
	topo.Roles.Worker = Assignment{EndpointID: "local", Model: "worker"}
	if !topo.IsReady() || topo.EffectiveCouncil().Model != "king" {
		t.Fatalf("ready/fallback failed: %#v", topo)
	}
	topo.Roles.Council = Assignment{EndpointID: "local", Model: "council"}
	if topo.EffectiveCouncil().Model != "council" {
		t.Fatal("council override failed")
	}
}

func TestEndpointURLPolicies(t *testing.T) {
	accepted := []string{"http://localhost:1", "https://127.0.0.1/x", "http://[::1]:9", "http://10.0.0.1", "http://192.168.1.2", "http://172.16.0.1", "http://169.254.1.1", "http://[fd00::1]", "http://printer.local/v1"}
	for _, u := range accepted {
		if err := (Endpoint{ID: "x", Name: "x", Kind: KindOllama, BaseURL: u}).Validate(); err != nil {
			t.Errorf("accepted %s: %v", u, err)
		}
	}
	rejected := []string{"https://example.com", "http://8.8.8.8", "ftp://localhost", "http://user:pass@localhost", "http://localhost?q=1", "http://localhost#f", "://bad", "http://"}
	for _, u := range rejected {
		if err := (Endpoint{ID: "x", Name: "x", Kind: KindOllama, BaseURL: u}).Validate(); err == nil {
			t.Errorf("rejected %s", u)
		}
	}
}

func TestTopologyValidationRefsAndDuplicates(t *testing.T) {
	topo := Topology{Endpoints: []Endpoint{{ID: "a", Name: "A", Kind: KindOllama, BaseURL: "http://localhost"}, {ID: "a", Name: "B", Kind: KindOpenAICompatible, BaseURL: "http://localhost"}}}
	if err := topo.Validate(); err == nil {
		t.Fatal("duplicate endpoint accepted")
	}
	topo.Endpoints = topo.Endpoints[:1]
	topo.Roles.King = Assignment{EndpointID: "missing", Model: "m"}
	if err := topo.Validate(); err == nil {
		t.Fatal("missing ref accepted")
	}
}

func TestValidationOrderAndPartialRoles(t *testing.T) {
	topo := Topology{Endpoints: []Endpoint{{ID: "a", Name: "A", Kind: KindOllama, BaseURL: "http://localhost"}}, Roles: Roles{
		King: Assignment{EndpointID: "missing"}, Worker: Assignment{EndpointID: "missing"}, Council: Assignment{EndpointID: "missing"},
	}}
	if err := topo.Validate(); err == nil || !strings.Contains(err.Error(), "king") {
		t.Fatalf("want deterministic king error, got %v", err)
	}
}

func TestEndpointIndependentFieldsAndURLs(t *testing.T) {
	base := Endpoint{ID: "x", Name: "X", Kind: KindOpenAICompatible, BaseURL: "http://localhost"}
	for name, mutate := range map[string]func(*Endpoint){
		"empty id":       func(e *Endpoint) { e.ID = "" },
		"blank id":       func(e *Endpoint) { e.ID = " \t" },
		"empty name":     func(e *Endpoint) { e.Name = "" },
		"blank name":     func(e *Endpoint) { e.Name = " \t" },
		"empty kind":     func(e *Endpoint) { e.Kind = "" },
		"unknown kind":   func(e *Endpoint) { e.Kind = "bad" },
		"empty base URL": func(e *Endpoint) { e.BaseURL = "" },
	} {
		e := base
		mutate(&e)
		if err := e.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	ollama := base
	ollama.Kind = KindOllama
	if err := ollama.Validate(); err != nil {
		t.Fatalf("Ollama endpoint rejected: %v", err)
	}
	for _, u := range []string{"http://localhost?", "http://localhost:bad", "http://localhost:", "http://user@localhost", "http://localhost/path#frag"} {
		e := base
		e.BaseURL = u
		if err := e.Validate(); err == nil {
			t.Errorf("invalid URL accepted: %s", u)
		}
	}
}

func TestDuplicateEndpointError(t *testing.T) {
	topo := Topology{Endpoints: []Endpoint{
		{ID: "same", Name: "One", Kind: KindOllama, BaseURL: "http://localhost"},
		{ID: "same", Name: "Two", Kind: KindOllama, BaseURL: "http://localhost"},
	}}
	if err := topo.Validate(); err == nil || err.Error() != `duplicate endpoint id "same"` {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestEndpointURLBoundaryMatrix(t *testing.T) {
	base := Endpoint{ID: "x", Name: "x", Kind: KindOpenAICompatible}
	accepted := []string{"http://localhost", "http://127.0.0.1", "http://[::1]", "http://10.0.0.1", "http://169.254.1.1", "http://[fe80::1]", "http://[fd00::1]", "http://MiXeD.local/v1", "http://localhost:1", "http://localhost:65535"}
	for _, raw := range accepted {
		e := base
		e.BaseURL = raw
		if err := e.Validate(); err != nil {
			t.Errorf("accepted %q: %v", raw, err)
		}
	}
	rejected := []string{"http://example.com", "http://8.8.8.8", "http://[2001:db8::1]", "http://printer", "http://.local", "http://", "http:///x", "http://localhost:0", "http://localhost:65536", "http://localhost:bad", "http://localhost:", "http://u:p@localhost", "http://localhost?q=1", "http://localhost?", "http://localhost#f", "ftp://localhost"}
	for _, raw := range rejected {
		e := base
		e.BaseURL = raw
		if err := e.Validate(); err == nil {
			t.Errorf("rejected %q", raw)
		}
	}
}

func TestAssignmentErrorsByRole(t *testing.T) {
	topo := Topology{Endpoints: []Endpoint{{ID: "a", Name: "A", Kind: KindOllama, BaseURL: "http://localhost"}}}
	roles := []struct {
		name string
		set  func(*Roles, Assignment)
	}{{"king", func(r *Roles, a Assignment) { r.King = a }}, {"worker", func(r *Roles, a Assignment) { r.Worker = a }}, {"council", func(r *Roles, a Assignment) { r.Council = a }}}
	for _, role := range roles {
		for _, a := range []Assignment{{EndpointID: "a"}, {Model: "m"}} {
			var r Roles
			role.set(&r, a)
			topo.Roles = r
			if err := topo.Validate(); err == nil || err.Error() != role.name+" assignment incomplete" {
				t.Errorf("%s partial: %v", role.name, err)
			}
		}
		var r Roles
		role.set(&r, Assignment{EndpointID: "missing", Model: "m"})
		topo.Roles = r
		want := role.name + " assignment references unknown endpoint \"missing\""
		if err := topo.Validate(); err == nil || err.Error() != want {
			t.Errorf("%s unknown: %v", role.name, err)
		}
	}
}

func TestAssignmentErrorDeterministic(t *testing.T) {
	topo := Topology{Roles: Roles{King: Assignment{EndpointID: "x"}, Worker: Assignment{EndpointID: "y"}, Council: Assignment{EndpointID: "z"}}}
	for i := 0; i < 20; i++ {
		if err := topo.Validate(); err == nil || err.Error() != "king assignment incomplete" {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestOfflineEndpointDoesNotAffectReadiness(t *testing.T) {
	topo := Topology{Endpoints: []Endpoint{{ID: "a", Name: "A", Kind: KindOllama, BaseURL: "http://localhost"}}, Roles: Roles{King: Assignment{EndpointID: "a", Model: "k"}, Worker: Assignment{EndpointID: "a", Model: "w"}}}
	if !topo.IsReady() {
		t.Fatal("offline-independent readiness failed")
	}
}
