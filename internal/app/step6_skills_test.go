package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/topology"
)

type fakeSkillLibrary struct {
	dir    string
	skills []skills.Skill
	err    error
	loads  int
}

func (l *fakeSkillLibrary) Dir() string { return l.dir }
func (l *fakeSkillLibrary) Load() ([]skills.Skill, error) {
	l.loads++
	return append([]skills.Skill(nil), l.skills...), l.err
}

func TestControlKOpensSkillsBrowserWithPartialResults(t *testing.T) {
	library := &fakeSkillLibrary{
		dir:    "/tmp/kingdom-skills",
		skills: []skills.Skill{{Name: "careful-coder", Description: "Test first.", Instructions: "Write the failing test."}},
		err:    errors.New("bad.md: malformed"),
	}
	m := NewWithServices(completeConfig(), Services{Skills: library})
	m, _ = update(m, key("ctrl+k"))
	view := m.View().Content
	if !m.skills.open || library.loads != 1 || !strings.Contains(view, "careful-coder") || !strings.Contains(view, "Test first.") || !strings.Contains(view, library.dir) || !strings.Contains(view, "bad.md") {
		t.Fatalf("skills browser state=%+v view=%s", m, view)
	}
}

func TestSkillsBrowserNavigationToggleAndEscape(t *testing.T) {
	library := &fakeSkillLibrary{skills: []skills.Skill{
		{Name: "alpha", Instructions: "A"},
		{Name: "beta", Instructions: "B"},
	}}
	m := NewWithServices(completeConfig(), Services{Skills: library})
	m, _ = update(m, key("ctrl+k"))
	m, _ = update(m, key("down"))
	m, _ = update(m, key("enter"))
	if len(m.skills.active) != 1 || m.skills.active[0].Name != "beta" || !strings.Contains(strings.Join(m.history, "\n"), "skill active: beta") {
		t.Fatalf("active=%+v history=%+v", m.skills.active, m.history)
	}
	m, _ = update(m, key("enter"))
	if len(m.skills.active) != 0 {
		t.Fatalf("skill did not toggle off: %+v", m.skills.active)
	}
	m.chat.SetValue("unchanged")
	m, _ = update(m, key("x"))
	if m.chat.Value() != "unchanged" {
		t.Fatal("browser leaked key into chat")
	}
	m, _ = update(m, key("esc"))
	if m.skills.open {
		t.Fatal("escape did not close skills browser")
	}
}

func TestActiveSkillsAreSnapshottedForRun(t *testing.T) {
	library := &fakeSkillLibrary{skills: []skills.Skill{{Name: "careful-coder", Instructions: "Test first."}}}
	var received []skills.Skill
	services := Services{
		Skills: library,
		Run: func(_ context.Context, _ config.Config, _ string, _ string, active []skills.Skill) <-chan orchestration.Event {
			received = append([]skills.Skill(nil), active...)
			return nil
		},
	}
	m := NewWithServices(completeConfig(), services)
	m, _ = update(m, key("ctrl+k"))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("esc"))
	m.chat.SetValue("implement it")
	m, _ = update(m, key("ctrl+enter"))
	if len(received) != 1 || received[0].Name != "careful-coder" {
		t.Fatalf("run skills=%+v", received)
	}
	m.skills.active[0].Name = "mutated-after-submit"
	if received[0].Name != "careful-coder" {
		t.Fatal("run did not receive a skill snapshot")
	}
}

func TestSkillsCannotOpenDuringRunOrSetup(t *testing.T) {
	library := &fakeSkillLibrary{skills: []skills.Skill{{Name: "one", Instructions: "one"}}}
	m := NewWithServices(completeConfig(), Services{Skills: library})
	m.running = true
	m, _ = update(m, key("ctrl+k"))
	if m.skills.open || library.loads != 0 {
		t.Fatal("skills opened during active run")
	}
	m = NewWithServices(config.Default(), Services{Skills: library})
	m, _ = update(m, key("ctrl+k"))
	if m.skills.open {
		t.Fatal("skills opened during setup")
	}
}

func TestReloadRefreshesActiveSkillInstructions(t *testing.T) {
	library := &fakeSkillLibrary{skills: []skills.Skill{{Name: "changing", Instructions: "version one"}}}
	m := NewWithServices(completeConfig(), Services{Skills: library})
	m, _ = update(m, key("ctrl+k"))
	m, _ = update(m, key("enter"))
	library.skills[0].Instructions = "version two"
	m, _ = update(m, key("r"))
	if len(m.skills.active) != 1 || m.skills.active[0].Instructions != "version two" {
		t.Fatalf("active skills were stale after reload: %+v", m.skills.active)
	}
}

func TestSkillIntegrationLoadsActivatesAndReachesLocalKing(t *testing.T) {
	skillDir := t.TempDir()
	skillPath := filepath.Join(skillDir, "concise.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: concise\ndescription: Be brief.\n---\nAnswer in two sentences."), 0600); err != nil {
		t.Fatal(err)
	}
	library := skills.NewLibrary(skillDir, nil)
	systemPrompt := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []modelapi.Message `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		systemPrompt <- body.Messages[0].Content
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"message": map[string]string{"content": `{"type":"final","content":"brief answer"}`}})
	}))
	defer server.Close()

	configuration := completeConfig()
	configuration.Topology.Endpoints = []topology.Endpoint{{ID: "local", Name: "local", Kind: topology.KindOllama, BaseURL: server.URL}}
	configuration.Topology.Roles.King = topology.Assignment{EndpointID: "local", Model: "king"}
	configuration.Topology.Roles.Worker = topology.Assignment{EndpointID: "local", Model: "worker"}
	client := modelapi.NewClient()
	m := NewWithServices(configuration, Services{
		Skills: library,
		Run: func(ctx context.Context, cfg config.Config, _ string, prompt string, active []skills.Skill) <-chan orchestration.Event {
			return orchestration.NewEngine(cfg, client, orchestration.WithSkills(active)).Stream(ctx, prompt)
		},
	})
	m, _ = update(m, key("ctrl+k"))
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("esc"))
	m.chat.SetValue("hello")
	next, command := m.Update(key("ctrl+enter"))
	m = next.(Model)
	for command != nil && m.running {
		next, command = m.Update(command())
		m = next.(Model)
	}
	if prompt := <-systemPrompt; !strings.Contains(prompt, "Answer in two sentences.") {
		t.Fatalf("active skill missing from local King prompt: %q", prompt)
	}
	if !strings.Contains(m.View().Content, "brief answer") {
		t.Fatalf("final response missing: %s", m.View().Content)
	}
}
