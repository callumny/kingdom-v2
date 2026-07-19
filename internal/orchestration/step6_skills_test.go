package orchestration

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/topology"
)

type rolePromptClient struct {
	mu        sync.Mutex
	kingCalls int
	prompts   map[string][]modelapi.Message
}

func (c *rolePromptClient) Chat(_ context.Context, _ topology.Endpoint, model string, messages []modelapi.Message) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prompts[model] = append([]modelapi.Message(nil), messages...)
	switch model {
	case "king":
		c.kingCalls++
		if c.kingCalls == 1 {
			return `{"type":"delegate","tasks":[{"id":"one","prompt":"investigate"}]}`, nil
		}
		return `{"type":"final","content":"done"}`, nil
	case "council":
		return "review", nil
	default:
		return "worker result", nil
	}
}

func TestActiveSkillsAreBoundedAndInjectedIntoKingOnly(t *testing.T) {
	configuration := cfg()
	configuration.Topology.Roles.King.Model = "king"
	configuration.Topology.Roles.Worker.Model = "worker"
	configuration.Topology.Roles.Council = topology.Assignment{EndpointID: "e", Model: "council"}
	client := &rolePromptClient{prompts: make(map[string][]modelapi.Message)}
	active := []skills.Skill{{Name: "careful-coder", Description: "Test first.", Instructions: "Always write the failing test first."}}

	for range NewEngine(configuration, client, WithSkills(active)).Stream(context.Background(), "change code") {
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	kingSystem := client.prompts["king"][0].Content
	if !strings.Contains(kingSystem, "ACTIVE SKILLS") || !strings.Contains(kingSystem, "Always write the failing test first.") {
		t.Fatalf("King prompt missing skill: %q", kingSystem)
	}
	for _, role := range []string{"worker", "council"} {
		if strings.Contains(client.prompts[role][0].Content, "careful-coder") {
			t.Fatalf("%s received King skill instructions: %+v", role, client.prompts[role])
		}
	}
}

func TestEngineSnapshotsActiveSkills(t *testing.T) {
	active := []skills.Skill{{Name: "original", Instructions: "Original instructions."}}
	client := &messageRecordingClient{responses: []string{`{"type":"final","content":"done"}`}}
	engine := NewEngine(cfg(), client, WithSkills(active))
	active[0].Name = "mutated"
	active[0].Instructions = "Mutated instructions."

	for range engine.Stream(context.Background(), "p") {
	}
	client.mu.Lock()
	system := client.messages[0][0].Content
	client.mu.Unlock()
	if !strings.Contains(system, "original") || strings.Contains(system, "mutated") {
		t.Fatalf("skills were not snapshotted: %q", system)
	}
}
