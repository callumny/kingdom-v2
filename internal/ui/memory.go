package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/charmbracelet/x/ansi"
)

func MemoryView(width, height int, sessions []memory.Session, exchanges []memory.Exchange, cursor int, currentSessionID string, loading, confirming, compacting bool, loadError string) tea.View {
	contentWidth := chatContentWidth(width)
	header := royalBrand.Render("♛ SESSIONS")
	count := royalGreen.Render(fmt.Sprintf("%d SAVED", len(sessions)))
	if gap := contentWidth - ansi.StringWidth(header) - ansi.StringWidth(count); gap > 1 {
		header += strings.Repeat(" ", gap) + count
	}
	rule := royalRule.Render(strings.Repeat("─", contentWidth))
	lines := []string{header, rule, royalMuted.Render("Continue a conversation, start fresh, or compact older context."), ""}

	bodyHeight := 15
	if height > 0 {
		bodyHeight = max(4, height-9)
	}
	lines = append(lines, sessionBody(sessions, exchanges, cursor, currentSessionID, loading, compacting, loadError, contentWidth, bodyHeight)...)
	lines = append(lines, rule)
	if confirming {
		lines = append(lines, royalRed.Render("Delete this session permanently?  y Confirm  •  n Cancel"))
	} else {
		lines = append(lines,
			royalMuted.Render("Enter Resume  •  n New session  •  c Compact  •  d Delete"),
			royalMuted.Render("j/k Move  •  r Reload  •  Esc Back  •  Ctrl+C Quit"),
		)
	}
	return tea.NewView(fitChatLines(lines, width, contentWidth))
}

func sessionBody(sessions []memory.Session, exchanges []memory.Exchange, cursor int, currentSessionID string, loading, compacting bool, loadError string, width, height int) []string {
	if loadError != "" {
		return padLines([]string{royalRed.Render("Session warning: " + loadError)}, height)
	}
	if len(sessions) == 0 {
		message := "No saved sessions yet. Press n to start one."
		if loading {
			message = "Loading sessions…"
		}
		return padLines([]string{royalGold.Render(message)}, height)
	}
	if cursor < 0 || cursor >= len(sessions) {
		cursor = 0
	}
	if width < 96 {
		lines := sessionListLines(sessions, cursor, currentSessionID, width)
		lines = append(lines, "", royalGold.Render("Selected conversation"))
		lines = append(lines, sessionTranscriptLines(exchanges, width)...)
		if loading {
			lines = append(lines, royalMuted.Render("Loading conversation…"))
		}
		if compacting {
			lines = append(lines, royalGold.Render("Compacting session…"))
		}
		return tailAndPad(lines, height)
	}

	listWidth := min(56, width/2)
	detailWidth := width - listWidth - 3
	left := sessionListLines(sessions, cursor, currentSessionID, listWidth)
	right := []string{royalGold.Render("Selected conversation"), ""}
	if loading {
		right = append(right, royalMuted.Render("Loading conversation…"))
	} else {
		right = append(right, sessionTranscriptLines(exchanges, detailWidth)...)
	}
	if compacting {
		right = append(right, "", royalGold.Render("Compacting session…"))
	}
	left = padLines(left, height)
	right = tailAndPad(right, height)
	lines := make([]string, height)
	for index := 0; index < height; index++ {
		leftLine := ansi.Truncate(left[index], listWidth, "")
		lines[index] = leftLine + strings.Repeat(" ", listWidth-ansi.StringWidth(leftLine)) + "   " + ansi.Truncate(right[index], detailWidth, "")
	}
	return lines
}

func sessionListLines(sessions []memory.Session, cursor int, currentSessionID string, width int) []string {
	lines := []string{royalGold.Render("Recent sessions"), ""}
	for index, session := range sessions {
		pointer := "  "
		if index == cursor {
			pointer = "› "
		}
		title := oneLine(session.Preview, max(8, width-4))
		if title == "" {
			title = "Untitled session"
		}
		if session.ID == currentSessionID {
			title += "  " + royalGreen.Render("CURRENT")
		}
		lines = append(lines, royalCyan.Render(pointer+title))
		usagePrefix := ""
		if session.TokenUsageEstimated {
			usagePrefix = "~"
		}
		contextPercent := 0
		if session.ContextWindow > 0 {
			contextPercent = session.ContextTokens * 100 / session.ContextWindow
		}
		meta := fmt.Sprintf("  %s · %d turns · %s%s tokens · ~%d%% context",
			friendlySessionTime(session.UpdatedAt), session.ExchangeCount, usagePrefix, compactNumber(session.TotalTokens), contextPercent)
		lines = append(lines, royalMuted.Render(ansi.Truncate(meta, width, "")), "")
	}
	return lines
}

func sessionTranscriptLines(exchanges []memory.Exchange, width int) []string {
	if len(exchanges) == 0 {
		return []string{royalMuted.Render("No saved turns in this session.")}
	}
	start := max(0, len(exchanges)-4)
	lines := make([]string, 0, (len(exchanges)-start)*3)
	for _, exchange := range exchanges[start:] {
		lines = append(lines, styledParagraph("You: "+exchange.User, width, royalText)...)
		lines = append(lines, styledParagraph("King: "+exchange.Reply, width, royalCyan)...)
		lines = append(lines, "")
	}
	return lines
}

func friendlySessionTime(value time.Time) string {
	if value.IsZero() {
		return "Saved"
	}
	local := value.Local()
	now := time.Now().Local()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("Today 15:04")
	}
	return local.Format("02 Jan 15:04")
}

func compactNumber(value int) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	return fmt.Sprintf("%.1fk", float64(value)/1000)
}

func oneLine(value string, width int) string {
	value = strings.Join(strings.Fields(value), " ")
	return ansi.Truncate(value, width, "…")
}
