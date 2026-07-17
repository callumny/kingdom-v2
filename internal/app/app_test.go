package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewModelRendersFoundation(t *testing.T) {
	view := New().View()
	if view.Content == "" || !strings.Contains(view.Content, "Kingdom") {
		t.Fatalf("expected foundation view, got %q", view.Content)
	}
}

func TestUpdateQuitKeys(t *testing.T) {
	keys := []tea.Key{{Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}}
	for _, key := range keys {
		model, cmd := New().Update(tea.KeyPressMsg(key))
		if model == nil || cmd == nil {
			t.Fatalf("key %q did not request quit", key.String())
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Fatalf("key %q returned %T, want tea.QuitMsg", key.String(), msg)
		}
	}
}
