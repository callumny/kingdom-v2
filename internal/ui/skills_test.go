package ui

import (
	"strings"
	"testing"

	"github.com/callumny/kingdom/internal/skills"
)

func TestSkillsViewShowsSelectionActivationAndControls(t *testing.T) {
	view := SkillsView(100, 30, []skills.Skill{
		{Name: "careful-coder", Description: "Test first.", Instructions: "Write a failing test.", BuiltIn: true},
		{Name: "concise", Description: "Be brief.", Instructions: "Use two sentences."},
	}, map[string]bool{"concise": true}, 1, "bad.md: malformed", "/tmp/skills").Content
	for _, expected := range []string{"Kingdom Skills", "[x] concise", "Be brief.", "Use two sentences.", "bad.md", "/tmp/skills", "Enter toggle"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
}

func TestSkillsViewRespectsTerminalBounds(t *testing.T) {
	view := SkillsView(12, 4, []skills.Skill{{Name: "a-very-long-skill", Instructions: "body"}}, nil, 0, "", "dir").Content
	lines := strings.Split(view, "\n")
	if len(lines) > 4 {
		t.Fatalf("height=%d", len(lines))
	}
	for _, line := range lines {
		if len([]rune(line)) > 12 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}
