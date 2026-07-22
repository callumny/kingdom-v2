package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type ChatInput struct{ Model textarea.Model }

type ChatModelActivity struct {
	Provider        string
	Model           string
	Roles           string
	Status          string
	TokensPerSecond float64
}

type ChatPresentation struct {
	History  []string
	Progress string
	Error    string
	Input    ChatInput
	Running  bool
	Models   []ChatModelActivity
}

func NewChatInput() ChatInput {
	t := textarea.New()
	t.Placeholder = "Ask the Kingdom…"
	t.CharLimit = 32 * 1024
	t.ShowLineNumbers = false
	t.Prompt = "> "
	t.SetHeight(3)
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
	return ChatViewWithPresentation(width, height, ChatPresentation{History: history, Progress: progress, Error: errorText, Input: input, Running: running})
}

func ChatViewWithPresentation(width, height int, p ChatPresentation) tea.View {
	contentWidth := chatContentWidth(width)
	header := royalBrand.Render("♛ KINGDOM")
	status := royalGreen.Render("LOCAL · READY")
	if gap := contentWidth - ansi.StringWidth(header) - ansi.StringWidth(status); gap > 1 {
		header += strings.Repeat(" ", gap) + status
	}
	rule := royalRule.Render(strings.Repeat("─", contentWidth))
	footer := []string{
		royalMuted.Render("Ctrl+Enter Send  •  /setup Setup  •  /models Models"),
		royalMuted.Render("/memory Memory  •  /skills Skills  •  Ctrl+C Quit"),
	}
	inputLines := strings.Split(p.Input.View(), "\n")
	fixed := 2 + 1 + len(inputLines) + 1 + len(footer)
	bodyHeight := 16
	if height > 0 {
		bodyHeight = height - fixed
		if bodyHeight < 1 {
			bodyHeight = 1
		}
	}
	body := chatBody(p, contentWidth, bodyHeight)
	lines := []string{header, rule}
	lines = append(lines, body...)
	lines = append(lines, royalGold.Render("Ask the Kingdom"))
	lines = append(lines, inputLines...)
	lines = append(lines, rule)
	lines = append(lines, footer...)
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return tea.NewView(fitChatLines(lines, width, contentWidth))
}

func chatBody(p ChatPresentation, width, height int) []string {
	conversation := []string{royalGold.Render("Conversation"), ""}
	for _, message := range p.History {
		style := royalCyan
		if strings.HasPrefix(message, "You: ") {
			style = royalText
		}
		conversation = append(conversation, styledParagraph(message, max(1, width-2), style)...)
	}
	if p.Progress != "" {
		conversation = append(conversation, royalGold.Render(p.Progress))
	}
	if p.Error != "" {
		conversation = append(conversation, royalRed.Render("Error: "+p.Error))
	}
	if p.Running && p.Progress == "" {
		conversation = append(conversation, royalGold.Render("Running…"))
	}

	if width < 96 {
		conversation = append(conversation, "")
		conversation = append(conversation, chatActivityLines(p.Models, width)...)
		return tailAndPad(conversation, height)
	}
	activityWidth := min(38, width/3)
	conversationWidth := width - activityWidth - 3
	conversation = rewrapChatLines(conversation, conversationWidth)
	activity := chatActivityLines(p.Models, activityWidth)
	conversation = tailAndPad(conversation, height)
	activity = padLines(activity, height)
	lines := make([]string, height)
	for index := 0; index < height; index++ {
		left := ansi.Truncate(conversation[index], conversationWidth, "")
		lines[index] = left + strings.Repeat(" ", conversationWidth-ansi.StringWidth(left)) + "   " + ansi.Truncate(activity[index], activityWidth, "")
	}
	return lines
}

func chatActivityLines(models []ChatModelActivity, width int) []string {
	lines := []string{royalGold.Render("Model activity"), ""}
	if len(models) == 0 {
		return append(lines, royalMuted.Render("No assigned models"))
	}
	for index, model := range models {
		lines = append(lines,
			royalCyan.Render(model.Provider+" · "+model.Model),
			royalText.Render(model.Roles),
		)
		speed := "— tok/s"
		if model.TokensPerSecond > 0 {
			speed = fmt.Sprintf("%.1f tok/s", model.TokensPerSecond)
		}
		status := model.Status
		if status == "" {
			status = "Ready"
		}
		lines = append(lines, royalMuted.Render(speed+" · "+status))
		if index+1 < len(models) {
			lines = append(lines, "")
		}
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	return lines
}

func rewrapChatLines(lines []string, width int) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			result = append(result, line)
			continue
		}
		result = append(result, ansi.Truncate(line, width, ""))
	}
	return result
}

func tailAndPad(lines []string, height int) []string {
	if len(lines) > height {
		keepTitle := lines[0]
		lines = append([]string{keepTitle}, lines[len(lines)-height+1:]...)
	}
	return padLines(lines, height)
}

func padLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func chatContentWidth(width int) int {
	if width <= 0 {
		return 116
	}
	if width < 20 {
		return width
	}
	contentWidth := width - 4
	if contentWidth > 124 {
		contentWidth = 124
	}
	if contentWidth < 1 {
		contentWidth = 1
	}
	return contentWidth
}

func fitChatLines(lines []string, terminalWidth, contentWidth int) string {
	leftPadding := 0
	if terminalWidth > contentWidth {
		leftPadding = (terminalWidth - contentWidth) / 2
	}
	padding := strings.Repeat(" ", leftPadding)
	for index, line := range lines {
		clipped := ansi.Truncate(line, contentWidth, "")
		if terminalWidth > 0 && terminalWidth < 6 {
			clipped = ansi.Truncate(ansi.Strip(line), contentWidth, "")
		}
		lines[index] = padding + clipped
	}
	return strings.Join(lines, "\n")
}
