package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
	"strings"
)

// CustomEndpointForm is a small, local-only endpoint form. q is intentionally
// handled by the focused text input; ctrl+c remains a global quit key.
type CustomEndpointForm struct {
	Name, BaseURL textinput.Model
	Kind          topology.EndpointKind
	Focus         int // 0 provider, 1 name, 2 URL
	Err           error
}

func (f CustomEndpointForm) View() string {
	kind := string(f.Kind)
	marker := func(i int) string {
		if f.Focus == i {
			return ">"
		}
		return " "
	}
	s := fmt.Sprintf("%s Provider: %s\n%s Name: %s\n%s URL: %s", marker(0), kind, marker(1), f.Name.Value(), marker(2), f.BaseURL.Value())
	if f.Err != nil {
		s += "\nError: " + f.Err.Error()
	}
	return s
}

func NewCustomEndpointForm() CustomEndpointForm {
	n := textinput.New()
	n.Prompt = "Name: "
	u := textinput.New()
	u.Prompt = "URL: "
	n.Focus()
	return CustomEndpointForm{Name: n, BaseURL: u, Kind: topology.KindOllama, Focus: 1}
}
func (f CustomEndpointForm) Init() tea.Cmd { return nil }
func (f CustomEndpointForm) Update(msg tea.Msg) (CustomEndpointForm, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "tab":
			f.Focus = (f.Focus + 1) % 3
			if f.Focus == 1 {
				f.Name.Focus()
				f.BaseURL.Blur()
			} else if f.Focus == 2 {
				f.BaseURL.Focus()
				f.Name.Blur()
			} else {
				f.Name.Blur()
				f.BaseURL.Blur()
			}
		case "left", "right", " ":
			if f.Focus == 0 {
				if f.Kind == topology.KindOllama {
					f.Kind = topology.KindOpenAICompatible
				} else {
					f.Kind = topology.KindOllama
				}
				return f, nil
			}
		case "o":
			if f.Focus == 0 {
				if f.Kind == topology.KindOllama {
					f.Kind = topology.KindOpenAICompatible
				} else {
					f.Kind = topology.KindOllama
				}
				return f, nil
			}
		case "esc":
			return f, nil
		}
	}
	var c tea.Cmd
	if f.Focus == 1 {
		f.Name, c = f.Name.Update(msg)
	} else {
		f.BaseURL, c = f.BaseURL.Update(msg)
	}
	return f, c
}
func (f CustomEndpointForm) Endpoint() (topology.Endpoint, error) {
	return setup.ValidateCustom(f.Kind, strings.TrimSpace(f.Name.Value()), strings.TrimSpace(f.BaseURL.Value()))
}
