// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dqliteDetailModel struct {
	width          int
	height         int
	active         bool
	ddl            string
	queryInput     textarea.Model
	queryColumns   []string
	queryRows      [][]string
	queryCount     int
	queryTruncated bool
	queryError     string
	ddlViewport    viewport.Model
	resultsViewport viewport.Model
	ready          bool
}

func newDqliteDetailModel() dqliteDetailModel {
	ti := textarea.New()
	ti.Placeholder = "SELECT ..."
	ti.ShowLineNumbers = false
	ti.CharLimit = 0
	return dqliteDetailModel{queryInput: ti}
}

func (m dqliteDetailModel) Update(msg tea.Msg) (dqliteDetailModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case loadDDLMsg:
		if msg.err != nil {
			return m, nil
		}
		m.ddl = msg.ddl
		if m.ready {
			m.ddlViewport.SetContent(m.ddl)
			m.ddlViewport.GotoTop()
		}
		return m, nil

	case loadQueryMsg:
		if msg.err != nil {
			m.queryError = msg.err.Error()
		} else if msg.result != nil {
			m.queryColumns = msg.result.Columns
			m.queryRows = msg.result.Rows
			m.queryCount = msg.result.RowCount
			m.queryTruncated = msg.result.Truncated
			m.queryError = ""
		}
		if m.ready {
			m.resultsViewport.SetContent(m.renderResults())
			m.resultsViewport.GotoTop()
		}
		return m, nil

	case tea.KeyMsg:
		if m.active {
			switch msg.String() {
			case "ctrl+enter":
				break
			default:
				if m.queryInput.Focused() {
					var cmd tea.Cmd
					m.queryInput, cmd = m.queryInput.Update(msg)
					cmds = append(cmds, cmd)
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = m.Height()
		ddlHeight := max(m.height/3-2, 1)
		resultsHeight := max(m.height-m.height/3-5, 1)
		m.ddlViewport = viewport.New(m.width-4, ddlHeight)
		m.ddlViewport.SetContent(m.ddl)
		m.resultsViewport = viewport.New(m.width-4, resultsHeight)
		m.resultsViewport.SetContent(m.renderResults())
		m.queryInput.SetWidth(m.width - 6)
		m.queryInput.SetHeight(1)
		m.ready = true
	}

	if m.ready {
		var cmd tea.Cmd
		m.ddlViewport, cmd = m.ddlViewport.Update(msg)
		cmds = append(cmds, cmd)
		m.resultsViewport, cmd = m.resultsViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m dqliteDetailModel) Height() int {
	if m.height < 6 {
		return 6
	}
	return m.height
}

func (m dqliteDetailModel) renderResults() string {
	var b strings.Builder

	if m.queryError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		b.WriteString(errStyle.Render(m.queryError))
		b.WriteString("\n")
		return b.String()
	}

	if len(m.queryColumns) == 0 {
		return ""
	}

	colWidths := make([]int, len(m.queryColumns))
	for i, col := range m.queryColumns {
		colWidths[i] = len(col)
	}
	for _, row := range m.queryRows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	headerParts := make([]string, len(m.queryColumns))
	for i, col := range m.queryColumns {
		headerParts[i] = fmt.Sprintf("%-*s", colWidths[i], col)
	}
	b.WriteString(" ")
	b.WriteString(strings.Join(headerParts, " │ "))
	b.WriteString("\n")

	sepParts := make([]string, len(m.queryColumns))
	for i := range m.queryColumns {
		sepParts[i] = strings.Repeat("─", colWidths[i])
	}
	b.WriteString(" ")
	b.WriteString(strings.Join(sepParts, "─┼─"))
	b.WriteString("\n")

	for _, row := range m.queryRows {
		cellParts := make([]string, len(row))
		for i, cell := range row {
			w := 0
			if i < len(colWidths) {
				w = colWidths[i]
			}
			cellParts[i] = fmt.Sprintf("%-*s", w, cell)
		}
		b.WriteString(" ")
		b.WriteString(strings.Join(cellParts, " │ "))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf(" %d rows", m.queryCount))
	if m.queryTruncated {
		truncatedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString(truncatedStyle.Render("  (truncated)"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m dqliteDetailModel) View() string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	shortcutStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("228"))

	title := headerStyle.Render("DDL / Query")
	shortcut := shortcutStyle.Render("[^ENTER] run")
	headerLine := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", shortcut)

	sepWidth := max(m.width-4, 10)
	separator := strings.Repeat("─", sepWidth)

	var b strings.Builder
	b.WriteString(headerLine)
	b.WriteString("\n")

	if m.ready {
		b.WriteString(m.ddlViewport.View())
	} else {
		if m.ddl != "" {
			b.WriteString(m.ddl)
		} else {
			b.WriteString("(select an object to view DDL)")
		}
		b.WriteString("\n")
	}

	b.WriteString(separator)
	b.WriteString("\n")

	if m.active {
		b.WriteString(m.queryInput.View())
	} else {
		val := m.queryInput.Value()
		if val == "" {
			b.WriteString("SELECT ...")
		} else {
			b.WriteString(val)
		}
	}
	b.WriteString("\n")

	b.WriteString(separator)
	b.WriteString("\n")

	if m.ready {
		b.WriteString(m.resultsViewport.View())
	} else {
		if m.queryError != "" {
			errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			b.WriteString(errStyle.Render(m.queryError))
		} else if len(m.queryColumns) > 0 {
			b.WriteString(m.renderResults())
		}
	}

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

	return borderStyle.Render(b.String())
}
