package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestChatViewANSIClippingPreservesSequences(t *testing.T) {
	c := NewChatInput()
	c.SetValue("界界界")
	v := ChatView(8, 20, []string{"\x1b[31mstyled界界\x1b[0m"}, "", "", c, false).Content
	for _, line := range strings.Split(v, "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatalf("line width=%d: %q", ansi.StringWidth(line), line)
		}
	}
	if !strings.Contains(v, "\x1b[") || strings.Contains(ansi.Strip(v), "styled") == false {
		t.Fatalf("ANSI/text lost: %q", v)
	}
}

func TestChatInputAcceptsOrdinaryControlLetters(t *testing.T) {
	c := NewChatInput()
	for _, k := range []string{"q", "s", "a"} {
		c, _ = c.Update(tea.KeyPressMsg(tea.Key{Text: k}))
	}
	if c.Value() == "" {
		t.Fatal("ordinary control letters should remain usable by the input")
	}
}

func TestChatInputEnterCreatesNewline(t *testing.T) {
	c := NewChatInput()
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Text: "a"}))
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Text: "b"}))
	if c.Value() != "a\nb" {
		t.Fatalf("value=%q, want newline", c.Value())
	}
}

func TestChatInputRuneLimitPreservesUTF8(t *testing.T) {
	c := NewChatInput()
	c.SetValue(strings.Repeat("界", 40000))
	if got := []rune(c.Value()); len(got) != 32*1024 {
		t.Fatalf("runes=%d, want %d", len(got), 32*1024)
	}
	if strings.ToValidUTF8(c.Value(), "?") != c.Value() {
		t.Fatal("input was not valid UTF-8")
	}
}

func TestChatViewRendersHistoryProgressErrorAndControls(t *testing.T) {
	c := NewChatInput()
	v := ChatView(120, 30, []string{"You: hello", "King: world"}, "Workers running…", "boom", c, true).Content
	for _, want := range []string{"You: hello", "King: world", "Workers running…", "Error: boom", "Running…", "Ctrl+Enter send", "Ctrl+C quit"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q: %s", want, v)
		}
	}
}

func TestChatViewClipsTinyTerminal(t *testing.T) {
	v := ChatView(5, 2, []string{"one", "two", "three"}, "", "", NewChatInput(), false).Content
	if len(strings.Split(v, "\n")) > 2 {
		t.Fatalf("lines exceed height: %q", v)
	}
	for _, line := range strings.Split(v, "\n") {
		if len([]rune(line)) > 5 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}
