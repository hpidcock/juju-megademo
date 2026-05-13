// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type helpModel struct {
	width  int
	height int
}

func newHelpModel() helpModel {
	return helpModel{}
}

func (m helpModel) View() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(m.width - 4).
		Height(m.height - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("228")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	helpText := titleStyle.Render("Keybindings") + "\n\n"

	bindings := []struct {
		key  string
		desc string
	}{
		{"s", "Step forward 1 transaction"},
		{"S", "Step forward N transactions"},
		{"p", "Pause the changestream"},
		{"r", "Resume the changestream"},
		{"m", "Switch model"},
		{"Tab", "Switch to next model/controller tab"},
		{"Shift+Tab", "Switch to previous tab"},
		{"↑ / ↓", "Navigate transaction list"},
		{"Enter", "Select transaction for Trace Pane"},
		{"l", "Cycle log level"},
		{"/", "Filter by module"},
		{"Esc", "Cancel/clear filter input"},
		{"q", "Quit the TUI"},
		{"?", "Toggle this help overlay"},
	}

	for _, b := range bindings {
		helpText += fmt.Sprintf("  %s  %s\n",
			keyStyle.Render(fmt.Sprintf("%-12s", b.key)),
			descStyle.Render(b.desc),
		)
	}

	helpText += "\n" + descStyle.Render("Press ? or Esc to close this overlay.")

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		borderStyle.Render(helpText),
		lipgloss.WithWhitespaceChars(" "),
	)
}
