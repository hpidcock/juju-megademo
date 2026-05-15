// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/juju/juju/api/common"
)

type dqliteDBListModel struct {
	width     int
	height    int
	active    bool
	databases []common.DqliteDatabase
	cursor    int
	viewport  viewport.Model
	ready     bool
}

func newDqliteDBListModel() dqliteDBListModel {
	return dqliteDBListModel{}
}

func (m dqliteDBListModel) Update(msg tea.Msg) (dqliteDBListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case loadDatabasesMsg:
		if msg.err != nil {
			return m, nil
		}
		m.databases = msg.databases
		m.clampCursor()
		m.refreshViewport()
		return m, nil

	case tea.KeyMsg:
		if !m.active {
			return m, nil
		}
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.refreshViewport()
			return m, nil
		case "down":
			if m.cursor < len(m.databases)-1 {
				m.cursor++
			}
			m.refreshViewport()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = m.Height()
		vpHeight := max(m.height-4, 1)
		m.viewport = viewport.New(m.width-4, vpHeight)
		m.viewport.SetContent(m.renderRows())
		m.ready = true
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *dqliteDBListModel) refreshViewport() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.renderRows())
	m.syncViewportToCursor()
}

func (m *dqliteDBListModel) clampCursor() {
	if len(m.databases) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.databases) {
		m.cursor = len(m.databases) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *dqliteDBListModel) syncViewportToCursor() {
	if !m.ready || m.viewport.Height == 0 {
		return
	}
	visibleStart := m.viewport.YOffset
	visibleEnd := visibleStart + m.viewport.Height
	if m.cursor < visibleStart {
		m.viewport.SetYOffset(m.cursor)
	} else if m.cursor >= visibleEnd {
		m.viewport.SetYOffset(m.cursor - m.viewport.Height + 1)
	}
}

func (m dqliteDBListModel) Height() int {
	if m.height < 4 {
		return 4
	}
	return m.height
}

func (m dqliteDBListModel) renderRows() string {
	highlightStyle := lipgloss.NewStyle().Reverse(true)

	var rows strings.Builder
	for i, db := range m.databases {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s", marker, db.Name)
		if i == m.cursor {
			line = highlightStyle.Render(line)
		}
		rows.WriteString(line + "\n")
	}
	if len(m.databases) == 0 {
		rows.WriteString("  (none)\n")
	}
	return rows.String()
}

func (m dqliteDBListModel) View() string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	header := headerStyle.Render("Databases")

	borderColor := lipgloss.Color("62")
	if m.active {
		borderColor = lipgloss.Color("86")
	}
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		PaddingLeft(1).
		PaddingRight(1).
		Width(m.width - 2).
		Height(m.Height() - 2)

	var content string
	if m.ready {
		content = header + "\n" + m.viewport.View()
	} else {
		content = header + "\n" + m.renderRows()
	}

	return borderStyle.Render(content)
}
