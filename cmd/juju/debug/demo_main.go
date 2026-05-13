// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

//go:build ignore

package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/juju/juju/cmd/juju/debug"
)

func main() {
	api := debug.NewDqliteAPI()
	model := debug.NewDqliteModel(api)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, _ = p.Run()
}
