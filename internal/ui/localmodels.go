package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/charmbracelet/x/ansi"
)

func LocalModelsView(width, height int, runtimes []localmodels.Runtime, runtimeCursor, modelCursor int, loading, starting, confirming bool, errorText string) tea.View {
	lines := []string{"Local Models", ""}
	if errorText != "" {
		lines = append(lines, "Error: "+errorText, "")
	}
	if loading && len(runtimes) == 0 {
		lines = append(lines, "Inspecting local runtimes…")
	} else {
		for index, runtime := range runtimes {
			pointer := "  "
			if index == runtimeCursor {
				pointer = "> "
			}
			status := "stopped"
			if !runtime.Installed {
				status = "not installed"
			} else if runtime.Running {
				status = "running"
			}
			lines = append(lines, fmt.Sprintf("%s%s — %s", pointer, runtime.Name, status))
			if index == runtimeCursor {
				if !runtime.Installed && runtime.InstallHint != "" {
					lines = append(lines, "  "+runtime.InstallHint)
				}
				if runtime.Warning != "" {
					lines = append(lines, "  Warning: "+runtime.Warning)
				}
				if len(runtime.Models) == 0 && runtime.Installed {
					lines = append(lines, "  No models visible. Start or refresh the runtime.")
				}
				for modelIndex, model := range runtime.Models {
					modelPointer := "    "
					if modelIndex == modelCursor {
						modelPointer = "  > "
					}
					state := "installed"
					if model.Loaded {
						state = "loaded"
					}
					lines = append(lines, fmt.Sprintf("%s%s (%s)", modelPointer, model.ID, state))
				}
			}
		}
	}
	if starting {
		lines = append(lines, "", "Starting model and waiting for readiness…")
	} else if confirming {
		name := "selected runtime"
		if runtimeCursor >= 0 && runtimeCursor < len(runtimes) {
			name = runtimes[runtimeCursor].Name
		}
		lines = append(lines, "", "Start "+name+"? It will continue running after Kingdom exits. y confirm   n cancel")
	} else {
		lines = append(lines, "", "h/l runtime   j/k model   s start   Enter assign   r refresh   Esc back")
	}
	return tea.NewView(fitLocalModelsView(strings.Join(lines, "\n"), width, height))
}

func fitLocalModelsView(content string, width, height int) string {
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
