package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/ui"
)

type SkillLibrary interface {
	Dir() string
	Load() ([]skills.Skill, error)
}

type skillState struct {
	library   SkillLibrary
	open      bool
	available []skills.Skill
	active    []skills.Skill
	cursor    int
	err       string
	dir       string
}

func (m Model) handleSkillsKey(key string) Model {
	switch key {
	case "esc", "ctrl+k":
		m.skills.open = false
	case "r":
		m.loadSkills()
	case "down", "j":
		if len(m.skills.available) > 0 {
			m.skills.cursor = (m.skills.cursor + 1) % len(m.skills.available)
		}
	case "up", "k":
		if len(m.skills.available) > 0 {
			m.skills.cursor = (m.skills.cursor - 1 + len(m.skills.available)) % len(m.skills.available)
		}
	case "enter", " ":
		m.toggleCurrentSkill()
	}
	return m
}

func (m *Model) openSkills() {
	m.skills.open = true
	m.skills.cursor = 0
	m.loadSkills()
}

func (m *Model) loadSkills() {
	available, err := m.skills.library.Load()
	m.skills.available = available
	byName := make(map[string]skills.Skill, len(available))
	for _, skill := range available {
		byName[strings.ToLower(skill.Name)] = skill
	}
	refreshed := make([]skills.Skill, 0, len(m.skills.active))
	for _, active := range m.skills.active {
		if current, ok := byName[strings.ToLower(active.Name)]; ok {
			refreshed = append(refreshed, current)
		} else {
			m.history = append(m.history, "skill unavailable: "+active.Name)
		}
	}
	m.skills.active = refreshed
	m.skills.dir = m.skills.library.Dir()
	m.skills.err = ""
	if err != nil {
		m.skills.err = err.Error()
	}
	if len(available) == 0 {
		m.skills.cursor = 0
	} else if m.skills.cursor >= len(available) {
		m.skills.cursor = len(available) - 1
	}
}

func (m *Model) toggleCurrentSkill() {
	if m.skills.cursor < 0 || m.skills.cursor >= len(m.skills.available) {
		return
	}
	selected := m.skills.available[m.skills.cursor]
	for index, active := range m.skills.active {
		if strings.EqualFold(active.Name, selected.Name) {
			m.skills.active = append(m.skills.active[:index], m.skills.active[index+1:]...)
			m.history = append(m.history, "skill inactive: "+selected.Name)
			return
		}
	}
	m.skills.active = append(m.skills.active, selected)
	m.history = append(m.history, "skill active: "+selected.Name)
}

func (m Model) skillsView() tea.View {
	active := make(map[string]bool, len(m.skills.active))
	for _, skill := range m.skills.active {
		active[strings.ToLower(skill.Name)] = true
	}
	return ui.SkillsView(m.width, m.height, m.skills.available, active, m.skills.cursor, m.skills.err, m.skills.dir)
}
