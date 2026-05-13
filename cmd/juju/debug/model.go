// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pane int

const (
	changestreamPane pane = iota
	logPane
	tracePane
	numPanes
)

type debugModel struct {
	width          int
	height         int
	activePane     pane
	controllerName string
	modelName      string
	modelUUID      string

	changestream changestreamModel
	log          logModel
	trace        traceModel
	help         helpModel

	debugAPI        DebugChangeStreamAPI
	modelLister     ModelListAPI
	switchModelFunc func(modelName string) error

	showHelp bool
	quitting bool
}

func newDebugModel(
	controllerName, modelName, modelUUID string,
	logAPI LogAPI,
	debugAPI DebugChangeStreamAPI,
	modelLister ModelListAPI,
	switchModelFunc func(modelName string) error,
	tempoAPI TempoAPI,
) debugModel {
	return debugModel{
		controllerName:  controllerName,
		modelName:       modelName,
		modelUUID:       modelUUID,
		activePane:      changestreamPane,
		changestream:    newChangestreamModel(debugAPI, modelLister, modelName, modelUUID),
		log:             newLogModel(logAPI),
		trace:           newTraceModel(tempoAPI),
		help:            newHelpModel(),
		debugAPI:        debugAPI,
		modelLister:     modelLister,
		switchModelFunc: switchModelFunc,
	}
}

func (m debugModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.changestream.Init(),
		m.log.Init(),
	)
}

func (m debugModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.changestream.pickerOpen {
			var cmd tea.Cmd
			m.changestream, cmd = m.changestream.Update(msg)
			return m, cmd
		}

		if m.changestream.stepInputMode == stepInputActive {
			var cmd tea.Cmd
			m.changestream, cmd = m.changestream.Update(msg)
			return m, cmd
		}

		if m.log.filtering {
			switch msg.String() {
			case "esc":
				var cmd tea.Cmd
				m.log, cmd = m.log.Update(msg)
				return m, cmd
			case "enter":
				var cmd tea.Cmd
				m.log, cmd = m.log.Update(msg)
				return m, cmd
			default:
				var cmd tea.Cmd
				m.log, cmd = m.log.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "q":
			m.resumeAllPaused()
			if m.log.streamCancel != nil {
				m.log.streamCancel()
			}
			m.quitting = true
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
		case "tab":
			m.activePane = (m.activePane + 1) % numPanes
			m.syncActiveState()
			return m, nil
		case "shift+tab":
			m.activePane = (m.activePane - 1 + numPanes) % numPanes
			m.syncActiveState()
			return m, nil
		}

		switch m.activePane {
		case changestreamPane:
			var cmd tea.Cmd
			m.changestream, cmd = m.changestream.Update(msg)
			return m, cmd
		case logPane:
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(msg)
			return m, cmd
		case tracePane:
			var cmd tea.Cmd
			m.trace, cmd = m.trace.Update(msg)
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contextBarHeight := 1
		remainingHeight := m.height - contextBarHeight
		m.changestream.width = msg.Width
		m.changestream.height = min(paneHeight(remainingHeight, 0.40), 12)
		m.log.width = msg.Width
		m.log.height = paneHeight(remainingHeight, 0.35)
		m.trace.width = msg.Width
		m.trace.height = remainingHeight - min(paneHeight(remainingHeight, 0.40), 12) - paneHeight(remainingHeight, 0.35)
		m.help.width = msg.Width
		m.help.height = m.height
	}

	var cmd tea.Cmd
	var cmds []tea.Cmd

	m.changestream, cmd = m.changestream.Update(msg)
	cmds = append(cmds, cmd)

	m.log, cmd = m.log.Update(msg)
	cmds = append(cmds, cmd)

	m.trace, cmd = m.trace.Update(msg)
	cmds = append(cmds, cmd)

	if selectMsg, ok := msg.(selectTxnMsg); ok {
		m.trace.setTransaction(selectMsg.txn)
		var cmds []tea.Cmd
		if m.trace.spinning && m.trace.fetching != "" {
			cmds = append(cmds, m.trace.Init())
			cmds = append(cmds, fetchTraceCmd(m.trace.tempoAPI, m.trace.fetching))
		}
		return m, tea.Batch(cmds...)
	}

	if switchMsg, ok := msg.(switchModelMsg); ok {
		m.modelName = switchMsg.modelName
		m.modelUUID = switchMsg.modelUUID
		m.changestream.currentModel = switchMsg.modelUUID
		m.changestream.cursor = 0
		m.changestream.headerErr = ""
		m.trace = newTraceModel(m.trace.tempoAPI)
		if m.switchModelFunc != nil {
			_ = m.switchModelFunc(switchMsg.modelName)
		}
		cmds = append(cmds, m.restartLogStream()...)
	}

	return m, tea.Batch(cmds...)
}

func (m *debugModel) resumeAllPaused() {
	if m.debugAPI == nil {
		return
	}
	ctx := context.Background()
	for _, uuid := range m.changestream.pausedModelUUIDs() {
		_ = m.debugAPI.Resume(ctx, uuid)
	}
}

func (m *debugModel) restartLogStream() []tea.Cmd {
	if m.log.streamCancel != nil {
		m.log.streamCancel()
	}
	m.log.streamVersion++
	m.log.records = nil
	m.log.disconnected = false
	m.log.streamErr = ""
	return []tea.Cmd{startLogStreamCmd(m.log.logAPI, m.log.levelIndex, m.log.streamVersion)}
}

func (m *debugModel) syncActiveState() {
	m.changestream.active = m.activePane == changestreamPane
	m.log.active = m.activePane == logPane
	m.trace.active = m.activePane == tracePane
}

func (m debugModel) View() string {
	if m.quitting {
		return ""
	}

	if m.showHelp {
		return m.help.View()
	}

	contextBar := m.viewContextBar()
	changestreamView := m.changestream.View()
	logView := m.log.View()
	traceView := m.trace.View()

	view := contextBar + "\n" + changestreamView + "\n" + logView + "\n" + traceView

	if m.changestream.pickerOpen {
		pickerOverlay := m.changestream.viewPicker()
		view = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			pickerOverlay,
		)
	}

	return view
}

func (m debugModel) viewContextBar() string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	shortcutStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("228"))

	barStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(m.width)

	left := lipgloss.JoinHorizontal(lipgloss.Top,
		labelStyle.Render("Controller: "),
		valueStyle.Render(m.controllerName),
		labelStyle.Render("  Model: "),
		valueStyle.Render(m.modelName),
	)

	right := shortcutStyle.Render("[m]odel [q]uit")

	fullWidth := lipgloss.Width(left) + lipgloss.Width(right) + 4
	if fullWidth < m.width {
		padding := m.width - fullWidth
		right = lipgloss.NewStyle().PaddingLeft(padding).Render(right)
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return barStyle.Render(bar)
}

func paneHeight(totalHeight int, fraction float64) int {
	if totalHeight <= 0 {
		return 10
	}
	return int(float64(totalHeight) * fraction)
}
