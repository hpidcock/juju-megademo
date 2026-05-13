// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dqlitePane int

const (
	dqlitePaneDatabases dqlitePane = iota
	dqlitePaneObjects
	dqlitePaneQuery
	dqlitePaneCluster
)

type dqliteModel struct {
	width, height int
	focus         dqlitePane
	showHelp      bool
	quitting      bool
	err           string

	preSelectDatabase string
	defaultLimit      int

	databases  []DqliteDatabase
	selectedDB int

	kind        string
	objects     []DqliteObject
	selectedObj int

	ddl string

	queryInput     textarea.Model
	queryColumns   []string
	queryRows      [][]string
	queryCount     int
	queryTruncated bool
	queryError     string

	clusterNodes []DqliteNode

	api DqliteAPI
}

var (
	focusedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	unfocusedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("63"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	truncatedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	helpStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)
)

func NewDqliteModel(api DqliteAPI) *dqliteModel {
	m := &dqliteModel{
		focus:        dqlitePaneDatabases,
		kind:         "table",
		api:          api,
		defaultLimit: 100,
	}
	m.queryInput = textarea.New()
	m.queryInput.Placeholder = "SELECT ..."
	m.queryInput.ShowLineNumbers = false
	m.queryInput.CharLimit = 0
	return m
}

func (m *dqliteModel) Init() tea.Cmd {
	return tea.Batch(tea.EnterAltScreen, m.hardcodedLoadAllCmd())
}

func (m *dqliteModel) hardcodedLoadAllCmd() tea.Cmd {
	return func() tea.Msg {
		return hardcodedAllMsg{
			databases: []DqliteDatabase{
				{Name: "controller", Namespace: "controller", Type: "controller"},
				{Name: "lxd-pilot", UUID: "deadbeef-1234-5678-abcd-1234567890ab", Namespace: "deadbeef-1234-5678-abcd-1234567890ab", Type: "model"},
			},
			objects: []DqliteObject{
				{Name: "change_log", Kind: "table"},
				{Name: "model", Kind: "table"},
				{Name: "v_model_status", Kind: "view"},
			},
			ddl: "CREATE TABLE change_log (\n  id         INTEGER PRIMARY KEY,\n  edit_type_id INTEGER NOT NULL,\n  ns_id      INTEGER NOT NULL\n)",
			clusterNodes: []DqliteNode{
				{ID: "00ab1234", Address: "10.0.0.1:12345", Role: "voter"},
				{ID: "00cd5678", Address: "10.0.0.2:12345", Role: "stand-by"},
			},
			queryResult: &DqliteQueryResult{
				Columns:   []string{"id", "edit_type_id", "ns_id"},
				Rows:      [][]string{{"1", "1", "1"}, {"2", "2", "2"}, {"3", "3", "3"}},
				Count:     3,
				Truncated: true,
			},
		}
	}
}

type hardcodedAllMsg struct {
	databases    []DqliteDatabase
	objects      []DqliteObject
	ddl          string
	clusterNodes []DqliteNode
	queryResult  *DqliteQueryResult
}

func (m *dqliteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case hardcodedAllMsg:
		m.databases = msg.databases
		m.objects = msg.objects
		m.ddl = msg.ddl
		m.clusterNodes = msg.clusterNodes
		if msg.queryResult != nil {
			m.queryColumns = msg.queryResult.Columns
			m.queryRows = msg.queryResult.Rows
			m.queryCount = msg.queryResult.Count
			m.queryTruncated = msg.queryResult.Truncated
		}
		if m.preSelectDatabase != "" {
			for i, db := range m.databases {
				if db.Name == m.preSelectDatabase {
					m.selectedDB = i
					break
				}
			}
		}
		return m, nil

	case loadDatabasesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.databases = msg.databases
		if m.preSelectDatabase != "" {
			for i, db := range m.databases {
				if db.Name == m.preSelectDatabase {
					m.selectedDB = i
					break
				}
			}
		}
		if len(m.databases) > 0 {
			db := m.databases[m.selectedDB]
			return m, tea.Batch(
				loadObjectsCmd(m.api, db.Namespace, m.kind),
				loadClusterCmd(m.api),
			)
		}
		return m, nil

	case loadObjectsMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.objects = msg.objects
		m.selectedObj = 0
		return m, nil

	case loadDDLMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.ddl = msg.ddl
		return m, nil

	case loadQueryMsg:
		if msg.err != nil {
			m.queryError = msg.err.Error()
			return m, nil
		}
		m.queryColumns = msg.result.Columns
		m.queryRows = msg.result.Rows
		m.queryCount = msg.result.Count
		m.queryTruncated = msg.result.Truncated
		m.queryError = ""
		return m, nil

	case loadClusterMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.clusterNodes = msg.nodes
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.focus == dqlitePaneQuery {
		var cmd tea.Cmd
		m.queryInput, cmd = m.queryInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *dqliteModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "ctrl+h":
		m.showHelp = !m.showHelp
		return m, nil

	case "ctrl+r":
		return m, m.reloadActivePane()

	case "tab":
		m.focus = (m.focus + 1) % 4
		return m, nil

	case "shift+tab":
		m.focus = (m.focus + 3) % 4
		return m, nil

	case "esc":
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.focus == dqlitePaneQuery {
			m.queryInput.Blur()
			return m, nil
		}
		m.err = ""
		return m, nil
	}

	switch m.focus {
	case dqlitePaneDatabases:
		return m.handleDatabaseKeys(msg)
	case dqlitePaneObjects:
		return m.handleObjectKeys(msg)
	case dqlitePaneQuery:
		return m.handleQueryKeys(msg)
	case dqlitePaneCluster:
		return m, nil
	}

	return m, nil
}

func (m *dqliteModel) handleDatabaseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.selectedDB > 0 {
			m.selectedDB--
		}
		return m, nil
	case "down":
		if m.selectedDB < len(m.databases)-1 {
			m.selectedDB++
		}
		return m, nil
	case "enter":
		if len(m.databases) == 0 {
			return m, nil
		}
		db := m.databases[m.selectedDB]
		return m, loadObjectsCmd(m.api, db.Namespace, m.kind)
	}
	return m, nil
}

func (m *dqliteModel) handleObjectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.selectedObj > 0 {
			m.selectedObj--
		}
		return m, nil
	case "down":
		if m.selectedObj < len(m.objects)-1 {
			m.selectedObj++
		}
		return m, nil
	case "ctrl+1":
		m.kind = "table"
		return m, m.reloadObjects()
	case "ctrl+2":
		m.kind = "view"
		return m, m.reloadObjects()
	case "ctrl+3":
		m.kind = "trigger"
		return m, m.reloadObjects()
	case "enter":
		if len(m.objects) == 0 {
			return m, nil
		}
		obj := m.objects[m.selectedObj]
		db := m.databases[m.selectedDB]
		return m, loadDDLCmd(m.api, db.Namespace, obj.Name)
	}
	return m, nil
}

func (m *dqliteModel) handleQueryKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+enter":
		if len(m.databases) == 0 {
			return m, nil
		}
		db := m.databases[m.selectedDB]
		return m, loadQueryCmd(m.api, db.Namespace, m.queryInput.Value(), m.defaultLimit)
	default:
		var cmd tea.Cmd
		m.queryInput, cmd = m.queryInput.Update(msg)
		return m, cmd
	}
}

func (m *dqliteModel) reloadActivePane() tea.Cmd {
	if len(m.databases) == 0 {
		return nil
	}
	db := m.databases[m.selectedDB]
	switch m.focus {
	case dqlitePaneDatabases:
		return loadDatabasesCmd(m.api)
	case dqlitePaneObjects:
		return loadObjectsCmd(m.api, db.Namespace, m.kind)
	case dqlitePaneQuery:
		if m.queryInput.Value() != "" {
			return loadQueryCmd(m.api, db.Namespace, m.queryInput.Value(), m.defaultLimit)
		}
	case dqlitePaneCluster:
		return loadClusterCmd(m.api)
	}
	return nil
}

func (m *dqliteModel) reloadObjects() tea.Cmd {
	if len(m.databases) == 0 {
		return nil
	}
	db := m.databases[m.selectedDB]
	return loadObjectsCmd(m.api, db.Namespace, m.kind)
}

func (m *dqliteModel) View() string {
	if m.quitting {
		return ""
	}

	if m.showHelp {
		return m.viewHelp()
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	dbPane := m.viewDatabasesPane()
	objPane := m.viewObjectsPane()
	queryPane := m.viewQueryPane()
	clusterPane := m.viewClusterPane()

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, dbPane, objPane, queryPane)
	fullView := lipgloss.JoinVertical(lipgloss.Left, topRow, clusterPane)

	statusBar := m.viewStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, fullView, statusBar)
}

func (m *dqliteModel) paneStyle(pane dqlitePane) lipgloss.Style {
	if m.focus == pane {
		return focusedStyle
	}
	return unfocusedStyle
}

func (m *dqliteModel) viewDatabasesPane() string {
	paneWidth := m.width / 5
	paneHeight := m.height * 2 / 3 - 2

	style := m.paneStyle(dqlitePaneDatabases).Width(paneWidth).Height(paneHeight)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Databases"))
	b.WriteString("\n")

	if len(m.databases) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for i, db := range m.databases {
			cursor := " "
			if i == m.selectedDB {
				cursor = ">"
			}
			selected := " "
			if i == m.selectedDB {
				selected = "*"
			}
			b.WriteString(fmt.Sprintf(" %s%s %s\n", cursor, selected, db.Name))
		}
	}

	return style.Render(b.String())
}

func (m *dqliteModel) viewObjectsPane() string {
	paneWidth := m.width / 5
	paneHeight := m.height * 2 / 3 - 2

	style := m.paneStyle(dqlitePaneObjects).Width(paneWidth).Height(paneHeight)

	var b strings.Builder
	kindLabel := "Tables"
	switch m.kind {
	case "view":
		kindLabel = "Views"
	case "trigger":
		kindLabel = "Triggers"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("Objects [%s]", kindLabel)))
	b.WriteString("\n")

	if len(m.objects) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for i, obj := range m.objects {
			cursor := " "
			if i == m.selectedObj {
				cursor = ">"
			}
			b.WriteString(fmt.Sprintf(" %s %s\n", cursor, obj.Name))
		}
	}

	return style.Render(b.String())
}

func (m *dqliteModel) viewQueryPane() string {
	paneWidth := m.width * 3 / 5 - 4
	paneHeight := m.height * 2 / 3 - 2

	style := m.paneStyle(dqlitePaneQuery).Width(paneWidth).Height(paneHeight)

	var b strings.Builder

	b.WriteString(titleStyle.Render("DDL / Query"))
	b.WriteString("\n")

	if m.ddl != "" {
		b.WriteString(m.ddl)
		b.WriteString("\n")
	} else {
		b.WriteString("(select an object to view DDL)\n")
	}

	b.WriteString(strings.Repeat("─", min(paneWidth, 60)))
	b.WriteString("\n")

	if m.focus == dqlitePaneQuery {
		b.WriteString(m.queryInput.View())
	} else {
		b.WriteString(m.queryInput.Value())
		if m.queryInput.Value() == "" {
			b.WriteString("SELECT ...")
		}
	}
	b.WriteString("  [^ENTER]\n")

	b.WriteString(strings.Repeat("─", min(paneWidth, 60)))
	b.WriteString("\n")

	if m.queryError != "" {
		b.WriteString(errorStyle.Render(m.queryError))
		b.WriteString("\n")
	} else if len(m.queryColumns) > 0 {
		b.WriteString(m.renderResultsTable())
	}

	return style.Render(b.String())
}

func (m *dqliteModel) renderResultsTable() string {
	var b strings.Builder

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
		b.WriteString(truncatedStyle.Render("  (truncated)"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m *dqliteModel) viewClusterPane() string {
	paneWidth := m.width - 4
	paneHeight := m.height / 3 - 2

	style := m.paneStyle(dqlitePaneCluster).Width(paneWidth).Height(paneHeight)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Cluster"))
	b.WriteString("\n")

	if len(m.clusterNodes) == 0 {
		b.WriteString("  (no cluster info)\n")
	} else {
		idW, addrW := 20, 30
		b.WriteString(fmt.Sprintf(" %-20s  %-30s  %s\n", "ID", "Address", "Role"))
		b.WriteString(fmt.Sprintf(" %s  %s  %s\n",
			strings.Repeat("─", idW), strings.Repeat("─", addrW), strings.Repeat("─", 10)))
		for _, node := range m.clusterNodes {
			b.WriteString(fmt.Sprintf(" %-20s  %-30s  %s\n", node.ID, node.Address, node.Role))
		}
	}

	return style.Render(b.String())
}

func (m *dqliteModel) viewStatusBar() string {
	var b strings.Builder
	b.WriteString(" ^1/^2/^3 obj kind")
	b.WriteString("  Tab focus")
	b.WriteString("  ^R refresh")
	b.WriteString("  ^H help")
	b.WriteString("  ^C quit")

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(m.err))
	}

	return b.String()
}

func (m *dqliteModel) viewHelp() string {
	helpText := `  Tab          Next pane                   Ctrl+1..3  Object kind
  Shift+Tab    Previous pane               Ctrl+H     This help
  ↑/↓          Navigate list               Ctrl+R     Refresh pane
  Enter        Select database / object    Esc        Dismiss
  Ctrl+Enter   Execute query              Ctrl+C     Quit`

	return helpStyle.Render(helpText)
}
