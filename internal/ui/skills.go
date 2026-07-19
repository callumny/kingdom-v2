package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/charmbracelet/x/ansi"
)

func SkillsView(width, height int, available []skills.Skill, active map[string]bool, cursor int, loadError, directory string) tea.View {
	lines := []string{"Kingdom Skills", ""}
	if loadError != "" {
		lines = append(lines, "Load warning: "+loadError, "")
	}
	if len(available) == 0 {
		lines = append(lines, "No skills found.")
	} else {
		for index, skill := range available {
			pointer := "  "
			if index == cursor {
				pointer = "> "
			}
			mark := "[ ]"
			if active[strings.ToLower(skill.Name)] {
				mark = "[x]"
			}
			typeLabel := "user"
			if skill.BuiltIn {
				typeLabel = "built-in"
			}
			lines = append(lines, fmt.Sprintf("%s%s %s (%s)", pointer, mark, skill.Name, typeLabel))
		}
		if cursor >= 0 && cursor < len(available) {
			selected := available[cursor]
			lines = append(lines, "", selected.Description, "", selected.Instructions)
		}
	}
	lines = append(lines, "", "Skill directory: "+directory, "Enter toggle   j/k move   r reload   Esc back   Ctrl+C quit")
	return tea.NewView(fitSkillsView(strings.Join(lines, "\n"), width, height))
}

func fitSkillsView(content string, width, height int) string {
	lines := strings.Split(content, "\n")
	if width > 0 {
		for index, line := range lines {
			lines[index] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}
