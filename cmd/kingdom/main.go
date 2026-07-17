package main

import (
	"log"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/app"
)

func main() {
	program := tea.NewProgram(app.New())
	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}
