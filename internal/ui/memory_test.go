package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/memory"
)

func TestSessionsViewShowsReadablePreviewsUsageContextAndActions(t *testing.T) {
	sessions := []memory.Session{
		{ID: "current", Preview: "Plan the Kingdom demonstration", UpdatedAt: time.Now(), ExchangeCount: 6, TotalTokens: 4820, ContextTokens: 7500, ContextWindow: 32768},
		{ID: "older", Preview: "Investigate MLX startup", UpdatedAt: time.Now().Add(-24 * time.Hour), ExchangeCount: 3, TotalTokens: 1300, TokenUsageEstimated: true, ContextTokens: 2000, ContextWindow: 32768},
	}
	view := MemoryView(120, 30, sessions, []memory.Exchange{{User: "What should the demo show?", Reply: "Start with the user journey."}}, 0, "current", false, false, false, "").Content
	for _, expected := range []string{"♛ SESSIONS", "Plan the Kingdom demonstration", "CURRENT", "6 turns", "4.8k tokens", "~22% context", "Selected conversation", "What should the demo show?", "Enter Resume", "n New session", "c Compact"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q: %s", expected, view)
		}
	}
	assertViewFits(t, view, 120, 30)
}

func TestSessionsViewStacksOnNarrowTerminals(t *testing.T) {
	view := MemoryView(70, 28, []memory.Session{{ID: "one", Preview: "A readable session title", ExchangeCount: 1, ContextWindow: 32768}}, []memory.Exchange{{User: "hello", Reply: "welcome"}}, 0, "", false, false, false, "").Content
	if !strings.Contains(view, "Recent sessions") || !strings.Contains(view, "Selected conversation") || !strings.Contains(view, "welcome") {
		t.Fatalf("narrow view missing stacked content: %s", view)
	}
	assertViewFits(t, view, 70, 28)
}
