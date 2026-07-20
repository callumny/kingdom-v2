package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/callumny/kingdom/internal/topology"
)

func TestDefaultConfigAndLimits(t *testing.T) {
	c := Default()
	if c.Version != CurrentVersion || c.CouncilSize != 3 || c.WorkerConcurrency != 4 {
		t.Fatalf("defaults: %#v", c)
	}
	if c.IsReady() {
		t.Fatal("default ready")
	}
	if !c.RequiresSetup() {
		t.Fatal("default should require setup")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.CouncilSize = 0
	if err := c.Validate(); err == nil {
		t.Fatal("limit accepted")
	}
}

func TestLimitsAndCompleteReadiness(t *testing.T) {
	for _, n := range []int{1, 9} {
		c := complete()
		c.CouncilSize = n
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{0, 10} {
		c := complete()
		c.CouncilSize = n
		if err := c.Validate(); err == nil {
			t.Fatal("council bound accepted")
		}
	}
	for _, n := range []int{1, 32} {
		c := complete()
		c.WorkerConcurrency = n
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{0, 33} {
		c := complete()
		c.WorkerConcurrency = n
		if err := c.Validate(); err == nil {
			t.Fatal("worker bound accepted")
		}
	}
	c := complete()
	if c.RequiresSetup() {
		t.Fatal("complete config setup")
	}
}
func TestDefaultPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	p, err := DefaultPath()
	if err != nil || p != filepath.Join(home, ".kingdom", "v2", "config.json") {
		t.Fatalf("path %q %v", p, err)
	}
}
func complete() Config {
	c := Default()
	c.Providers.Ollama.Enabled = true
	c.Topology.Endpoints = []topology.Endpoint{{ID: "local", Name: "Local", Kind: topology.KindOllama, BaseURL: "http://localhost"}}
	c.Topology.Roles.King = topology.Assignment{EndpointID: "local", Model: "k"}
	c.Topology.Roles.Worker = topology.Assignment{EndpointID: "local", Model: "w"}
	c.Topology.Roles.Council = c.Topology.Roles.King
	return c
}
func TestStoreRoundTripAndMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, Default()) {
		t.Fatalf("missing: %#v %v", got, err)
	}
	c := complete()
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err = Load(p)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("roundtrip: %#v %v", got, err)
	}
}
func TestStoreStrictCorruptAndRejectedPreserve(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := complete()
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	for _, raw := range []string{`{"version":1,"extra":1}`, `{"version":`, `{"version":1}{"version":1}`} {
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
		got, _ := os.ReadFile(p)
		if string(got) != raw {
			t.Fatal("corrupt file rewritten")
		}
	}
	os.WriteFile(p, before, 0600)
	bad := c
	bad.CouncilSize = 99
	if err := Save(p, bad); err == nil {
		t.Fatal("invalid save accepted")
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Fatal("file changed on rejected save")
	}
}
func TestVersionAndAssignments(t *testing.T) {
	c := complete()
	if c.Version != CurrentVersion || c.Validate() != nil {
		t.Fatal("current version should validate")
	}
	c.Version = CurrentVersion + 1
	if err := c.Validate(); err == nil {
		t.Fatal("unknown version")
	}
	c = Default()
	c.Topology.Roles.King = topology.Assignment{EndpointID: "x"}
	if err := c.Validate(); err == nil {
		t.Fatal("incomplete assignment")
	}
}

func TestLoadTrailingWhitespaceAndScalarRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := complete()
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(raw, []byte(" \n\t")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("trailing whitespace rejected: %v", err)
	}
	scalar := append(append([]byte(nil), raw...), []byte(" 1")...)
	if err := os.WriteFile(p, scalar, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("trailing scalar accepted")
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(scalar) {
		t.Fatal("failed load changed file")
	}
}

func TestSaveCreatesRestrictedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not portable to Windows")
	}
	dir := filepath.Join(t.TempDir(), "new", "nested")
	p := filepath.Join(dir, "config.json")
	if err := Save(p, complete()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("dir mode: %v %v", info, err)
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode: %v %v", info, err)
	}
}

func TestSetupDefaultsAreExplicit(t *testing.T) {
	c := Default()
	if c.Providers.Ollama.Enabled || c.Providers.MLX.Enabled {
		t.Fatalf("providers should require an explicit choice: %+v", c.Providers)
	}
	if c.Providers.Ollama.Port != 11434 || c.Providers.MLX.Port != 8080 {
		t.Fatalf("provider ports: %+v", c.Providers)
	}
	if c.Providers.Ollama.PortMode != OllamaDedicatedPorts {
		t.Fatalf("Ollama port mode=%q, want %q", c.Providers.Ollama.PortMode, OllamaDedicatedPorts)
	}
	if !c.CouncilEnabled {
		t.Fatal("council should be offered by default")
	}
}

func TestCouncilReadinessIsAnExplicitChoice(t *testing.T) {
	c := complete()
	c.Providers.Ollama.Enabled = true
	c.CouncilEnabled = false
	c.Topology.Roles.Council = topology.Assignment{}
	if !c.IsReady() {
		t.Fatal("disabled council should not require a council model")
	}

	c.CouncilEnabled = true
	if c.IsReady() {
		t.Fatal("enabled council should require a council model")
	}
	c.Topology.Roles.Council = c.Topology.Roles.King
	if !c.IsReady() {
		t.Fatal("enabled council with an assignment should be ready")
	}
}

func TestLoadMigratesVersionOneWithoutChangingItsMeaning(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	raw := `{
  "version": 1,
  "council_size": 3,
  "worker_concurrency": 4,
  "topology": {
    "endpoints": [{"id":"ollama-local","name":"Ollama","kind":"ollama","base_url":"http://localhost:11434"}],
    "roles": {
      "king":{"endpoint_id":"ollama-local","model":"large"},
      "worker":{"endpoint_id":"ollama-local","model":"small"},
      "council":{"endpoint_id":"","model":""}
    }
  }
}`
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != CurrentVersion || !c.Providers.Ollama.Enabled || c.Providers.Ollama.PortMode != OllamaDedicatedPorts {
		t.Fatalf("migration defaults: %+v", c)
	}
	if !c.CouncilEnabled || c.Topology.Roles.Council != c.Topology.Roles.King {
		t.Fatalf("legacy council fallback was not preserved: %+v", c.Topology.Roles)
	}
	if !c.IsReady() {
		t.Fatal("a ready version-one config should remain ready")
	}
}
