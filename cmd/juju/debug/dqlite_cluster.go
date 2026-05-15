// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/juju/juju/api/common"
)

type dqliteClusterModel struct {
	width int
	nodes []common.DqliteNode
}

func newDqliteClusterModel() dqliteClusterModel {
	return dqliteClusterModel{}
}

func (m dqliteClusterModel) View() string {
	if m.width == 0 {
		return ""
	}

	topBorder := lipgloss.NewStyle().
		BorderTop(true).
		BorderForeground(lipgloss.Color("62")).
		Width(m.width).
		Render("")

	if len(m.nodes) == 0 {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		return topBorder + "\n" + labelStyle.Render("  Cluster: (no cluster info)")
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	idStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).Bold(true)

	roleStyle := func(role string) lipgloss.Style {
		switch role {
		case "voter":
			return lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		case "stand-by":
			return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		default:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
	}

	var entries []string
	for _, node := range m.nodes {
		abbrev := node.ID
		if len(abbrev) > 6 {
			abbrev = abbrev[:6] + "…"
		}
		entry := fmt.Sprintf("%s %s  %s",
			idStyle.Render(abbrev),
			node.Address,
			roleStyle(node.Role).Render(node.Role),
		)
		entries = append(entries, entry)
	}

	sep := labelStyle.Render(" │ ")
	line := labelStyle.Render("  Cluster  ") + strings.Join(entries, sep)

	return topBorder + "\n" + line
}
