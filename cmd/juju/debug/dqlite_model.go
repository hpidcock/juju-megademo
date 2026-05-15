// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/juju/juju/api/common"
)

type dqliteFocus int

const (
	dbFocusDatabases dqliteFocus = iota
	dbFocusObjects
	dbFocusDetail
	numDBFocusZones
)

type dqliteModel struct {
	width, height int
	focus        dqliteFocus
	showHelp     bool
	quitting     bool
	err          string

	preSelectDatabase string
	defaultLimit      int

	dbList  dqliteDBListModel
	objList dqliteObjListModel
	detail  dqliteDetailModel
	cluster dqliteClusterModel

	api DqliteAPI
}

func NewDqliteModel(api DqliteAPI) *dqliteModel {
	m := &dqliteModel{
		focus:        dbFocusDatabases,
		api:          api,
		defaultLimit: 100,
	}
	m.dbList = newDqliteDBListModel()
	m.objList = newDqliteObjListModel()
	m.detail = newDqliteDetailModel()
	m.cluster = newDqliteClusterModel()
	m.syncActiveState()
	return m
}

func (m *dqliteModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		loadDatabasesCmd(m.api),
	)
}

func (m *dqliteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutSubviews(msg)
		return m, nil

	case loadDatabasesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.dbList.databases = deduplicateDatabases(msg.databases)
		if m.preSelectDatabase != "" {
			for i, db := range m.dbList.databases {
				if db.Name == m.preSelectDatabase {
					m.dbList.cursor = i
					break
				}
			}
		}
		m.dbList.clampCursor()
		m.dbList.refreshViewport()
		if len(m.dbList.databases) > 0 {
			db := m.dbList.databases[m.dbList.cursor]
			return m, tea.Batch(
				loadObjectsCmd(m.api, db.Namespace, m.objList.kind),
				loadClusterCmd(m.api),
			)
		}
		return m, nil

	case loadObjectsMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.objList.objects = msg.objects
		m.objList.cursor = 0
		m.objList.refreshViewport()
		return m, nil

	case loadDDLMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.detail.ddl = msg.ddl
		if m.detail.ready {
			m.detail.ddlViewport.SetContent(m.detail.ddl)
			m.detail.ddlViewport.GotoTop()
		}
		return m, nil

	case loadQueryMsg:
		if msg.err != nil {
			m.detail.queryError = msg.err.Error()
		} else if msg.result != nil {
			m.detail.queryColumns = msg.result.Columns
			m.detail.queryRows = msg.result.Rows
			m.detail.queryCount = msg.result.RowCount
			m.detail.queryTruncated = msg.result.Truncated
			m.detail.queryError = ""
		}
		if m.detail.ready {
			m.detail.resultsViewport.SetContent(m.detail.renderResults())
			m.detail.resultsViewport.GotoTop()
		}
		return m, nil

	case loadClusterMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.cluster.nodes = msg.nodes
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m.propagate(msg)
}

func deduplicateDatabases(dbs []common.DqliteDatabase) []common.DqliteDatabase {
	controllerNames := make(map[string]bool)
	for _, db := range dbs {
		if db.Type == "controller" {
			controllerNames[db.Name] = true
		}
	}
	var result []common.DqliteDatabase
	for _, db := range dbs {
		if db.Type == "model" && controllerNames[db.Name] {
			continue
		}
		result = append(result, db)
	}
	return result
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
		m.focus = (m.focus + 1) % numDBFocusZones
		m.syncActiveState()
		return m, nil

	case "shift+tab":
		m.focus = (m.focus - 1 + numDBFocusZones) % numDBFocusZones
		m.syncActiveState()
		return m, nil

	case "esc":
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.focus == dbFocusDetail {
			m.detail.queryInput.Blur()
			return m, nil
		}
		m.err = ""
		return m, nil
	}

	switch m.focus {
	case dbFocusDatabases:
		return m.handleDBFocusKeys(msg)
	case dbFocusObjects:
		return m.handleObjFocusKeys(msg)
	case dbFocusDetail:
		return m.handleDetailFocusKeys(msg)
	}

	return m, nil
}

func (m *dqliteModel) handleDBFocusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "down":
		var cmd tea.Cmd
		m.dbList, cmd = m.dbList.Update(msg)
		return m, cmd
	case "enter":
		if len(m.dbList.databases) == 0 {
			return m, nil
		}
		db := m.dbList.databases[m.dbList.cursor]
		return m, loadObjectsCmd(m.api, db.Namespace, m.objList.kind)
	}
	return m, nil
}

func (m *dqliteModel) handleObjFocusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "down":
		var cmd tea.Cmd
		m.objList, cmd = m.objList.Update(msg)
		return m, cmd
	case "]":
		m.objList.cycleKind(1)
		return m, m.reloadObjects()
	case "[":
		m.objList.cycleKind(-1)
		return m, m.reloadObjects()
	case "enter":
		if len(m.objList.objects) == 0 || len(m.dbList.databases) == 0 {
			return m, nil
		}
		obj := m.objList.objects[m.objList.cursor]
		db := m.dbList.databases[m.dbList.cursor]
		return m, loadDDLCmd(m.api, db.Namespace, obj.Name)
	}
	return m, nil
}

func (m *dqliteModel) handleDetailFocusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyF5 {
		if len(m.dbList.databases) == 0 {
			return m, nil
		}
		db := m.dbList.databases[m.dbList.cursor]
		return m, loadQueryCmd(m.api, db.Namespace, m.detail.queryInput.Value(), m.defaultLimit)
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m *dqliteModel) reloadActivePane() tea.Cmd {
	if len(m.dbList.databases) == 0 {
		return nil
	}
	db := m.dbList.databases[m.dbList.cursor]
	switch m.focus {
	case dbFocusDatabases:
		return loadDatabasesCmd(m.api)
	case dbFocusObjects:
		return loadObjectsCmd(m.api, db.Namespace, m.objList.kind)
	case dbFocusDetail:
		if m.detail.queryInput.Value() != "" {
			return loadQueryCmd(m.api, db.Namespace, m.detail.queryInput.Value(), m.defaultLimit)
		}
	}
	if m.focus == dbFocusDatabases || m.focus == dbFocusObjects {
		return loadClusterCmd(m.api)
	}
	return nil
}

func (m *dqliteModel) reloadObjects() tea.Cmd {
	if len(m.dbList.databases) == 0 {
		return nil
	}
	db := m.dbList.databases[m.dbList.cursor]
	return loadObjectsCmd(m.api, db.Namespace, m.objList.kind)
}

func (m *dqliteModel) syncActiveState() {
	m.dbList.active = m.focus == dbFocusDatabases
	m.objList.active = m.focus == dbFocusObjects
	m.detail.active = m.focus == dbFocusDetail
	if m.detail.active {
		m.detail.queryInput.Focus()
	} else {
		m.detail.queryInput.Blur()
	}
}

func (m *dqliteModel) layoutSubviews(msg tea.WindowSizeMsg) {
	contextBarH := 1
	clusterBarH := 3
	mainH := m.height - contextBarH - clusterBarH

	leftW := m.width * 30 / 100
	rightW := m.width - leftW

	dbListH := mainH * 40 / 100
	objListH := mainH - dbListH

	m.dbList.width = leftW
	m.dbList.height = dbListH
	m.objList.width = leftW
	m.objList.height = objListH
	m.detail.width = rightW
	m.detail.height = mainH
	m.cluster.width = m.width

	dbVpMsg := tea.WindowSizeMsg{Width: leftW, Height: dbListH}
	objVpMsg := tea.WindowSizeMsg{Width: leftW, Height: objListH}
	detailVpMsg := tea.WindowSizeMsg{Width: rightW, Height: mainH}

	m.dbList, _ = m.dbList.Update(dbVpMsg)
	m.objList, _ = m.objList.Update(objVpMsg)
	m.detail, _ = m.detail.Update(detailVpMsg)
}

func (m *dqliteModel) propagate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	m.dbList, cmd = m.dbList.Update(msg)
	cmds = append(cmds, cmd)

	m.objList, cmd = m.objList.Update(msg)
	cmds = append(cmds, cmd)

	m.detail, cmd = m.detail.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
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

	contextBar := m.viewContextBar()

	leftCol := lipgloss.JoinVertical(lipgloss.Left, m.dbList.View(), m.objList.View())
	rightCol := m.detail.View()
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)

	clusterBar := m.cluster.View()

	view := lipgloss.JoinVertical(lipgloss.Left, contextBar, mainRow, clusterBar)

	if m.err != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		view += "\n" + errStyle.Render(m.err)
	}

	return view
}

func (m *dqliteModel) viewContextBar() string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	shortcutStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("228"))

	barStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(m.width)

	left := lipgloss.JoinHorizontal(lipgloss.Top,
		labelStyle.Render("Controller: "),
		valueStyle.Render("mycontroller"),
	)

	right := shortcutStyle.Render("[Tab] focus  [^H] help  [^C] quit")

	fullWidth := lipgloss.Width(left) + lipgloss.Width(right) + 4
	if fullWidth < m.width {
		padding := m.width - fullWidth
		right = lipgloss.NewStyle().PaddingLeft(padding).Render(right)
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return barStyle.Render(bar)
}

func (m *dqliteModel) viewHelp() string {
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
		{"Tab", "Switch pane"},
		{"Shift+Tab", "Previous pane"},
		{"↑ / ↓", "Navigate list / scroll"},
		{"Enter", "Select database / object"},
		{"F5", "Execute query"},
		{"[ / ]", "Cycle object kind (bracket keys)"},
		{"Ctrl+R", "Refresh pane"},
		{"Ctrl+H", "This help"},
		{"Esc", "Dismiss"},
		{"Ctrl+C", "Quit"},
	}

	for _, b := range bindings {
		helpText += fmt.Sprintf("  %s  %s\n",
			keyStyle.Render(fmt.Sprintf("%-14s", b.key)),
			descStyle.Render(b.desc),
		)
	}

	helpText += "\n" + descStyle.Render("Press Esc to close this overlay.")

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		borderStyle.Render(helpText),
		lipgloss.WithWhitespaceChars(" "),
	)
}
