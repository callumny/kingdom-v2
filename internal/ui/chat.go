package ui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type ChatInput struct{ Model textarea.Model }

func NewChatInput() ChatInput {
	t := textarea.New()
	t.Placeholder = "Ask the Kingdom…"
	t.CharLimit = 32 * 1024
	t.ShowLineNumbers = false
	t.Prompt = "> "
	t.Focus()
	return ChatInput{Model: t}
}
func (c ChatInput) Value() string { return c.Model.Value() }
func (c *ChatInput) SetValue(v string) {
	if rs := []rune(v); len(rs) > 32*1024 {
		v = string(rs[:32*1024])
	}
	c.Model.SetValue(v)
}
func (c ChatInput) Update(msg tea.Msg) (ChatInput, tea.Cmd) {
	var cmd tea.Cmd
	c.Model, cmd = c.Model.Update(msg)
	return c, cmd
}
func (c ChatInput) View() string { return c.Model.View() }
func ChatView(width, height int, history []string, progress, errorText string, input ChatInput, running bool) tea.View {
	lines := []string{"Kingdom"}
	for _, h := range history {
		lines = append(lines, h)
	}
	if progress != "" {
		lines = append(lines, progress)
	}
	if errorText != "" {
		lines = append(lines, "Error: "+errorText)
	}
	if running {
		lines = append(lines, "Running…")
	}
	lines = append(lines, "", input.View(), "Ctrl+Enter send   Ctrl+S setup   Esc cancel   Ctrl+C quit")
	content := strings.Join(lines, "\n")
	if height > 0 {
		ls := strings.Split(content, "\n")
		if len(ls) > height {
			ls = ls[len(ls)-height:]
			content = strings.Join(ls, "\n")
		}
	}
	if width > 0 {
		parts := strings.Split(content, "\n")
		for i, line := range parts {
			clipped := ansi.Truncate(line, width, "")
			// At extremely narrow widths, styling control sequences alone can
			// exceed the terminal's rune budget; drop styling rather than emit a
			// visually empty line that cannot fit legacy terminal constraints.
			if width < 6 && len([]rune(clipped)) > width {
				clipped = ansi.Truncate(ansi.Strip(line), width, "")
			}
			parts[i] = clipped
		}
		content = strings.Join(parts, "\n")
	}
	return tea.NewView(fmt.Sprintf("%s", content))
}
