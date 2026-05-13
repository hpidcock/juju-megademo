// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type transactionEntry struct {
	TxnID      int64
	EventCount int
	TraceID    string
	SpanID     string
}

type modelState struct {
	Name         string
	UUID         string
	Status       string
	TxnID        int64
	Transactions []transactionEntry
	Paused       bool
	PausedTxnIdx int
}

type stepInputMode int

const (
	stepInputNone stepInputMode = iota
	stepInputActive
)

type changestreamModel struct {
	width        int
	height       int
	active       bool
	currentModel string
	models       map[string]*modelState
	cursor       int

	pickerOpen      bool
	pickerItems     []ModelInfo
	pickerCursor    int
	controllerModel string

	api         DebugChangeStreamAPI
	modelLister ModelListAPI

	stepInputMode stepInputMode
	stepInput     textinput.Model
	headerErr     string

	viewport viewport.Model
	ready    bool
}

const maxTransactions = 10

const changestreamTickInterval = 500 * time.Millisecond

func newChangestreamModel(api DebugChangeStreamAPI, modelLister ModelListAPI, initialModelName, initialModelUUID string) changestreamModel {
	models := map[string]*modelState{
		initialModelUUID: {
			Name:         initialModelName,
			UUID:         initialModelUUID,
			Status:       "RUNNING",
			Transactions: nil,
			PausedTxnIdx: -1,
		},
	}

	ti := textinput.New()
	ti.Placeholder = "count"
	ti.CharLimit = 5
	ti.Prompt = "Step N: "

	return changestreamModel{
		active:       true,
		currentModel: initialModelUUID,
		models:       models,
		api:          api,
		modelLister:  modelLister,
		stepInputMode: stepInputNone,
		stepInput:    ti,
	}
}

func (m changestreamModel) Init() tea.Cmd {
	return tea.Batch(
		m.scheduleStatusPoll(),
		m.fetchModelsCmd(),
	)
}

func (m changestreamModel) scheduleStatusPoll() tea.Cmd {
	return tea.Tick(changestreamTickInterval, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}

func (m changestreamModel) fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := m.modelLister.ListModels(context.Background())
		return listModelsMsg{models: models, err: err}
	}
}

func (m changestreamModel) fetchModelsForPickerCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := m.modelLister.ListModels(context.Background())
		return listModelsMsg{models: models, err: err, open: true}
	}
}

func (m changestreamModel) pauseCmd(modelUUID string) tea.Cmd {
	return func() tea.Msg {
		err := m.api.Pause(context.Background(), modelUUID)
		return pauseResultMsg{err: err}
	}
}

func (m changestreamModel) pauseAllCmd() tea.Cmd {
	var cmds []tea.Cmd
	for uuid, ms := range m.models {
		if !ms.Paused {
			cmds = append(cmds, func(uuid string) tea.Cmd {
				return func() tea.Msg {
					err := m.api.Pause(context.Background(), uuid)
					return pauseResultMsg{err: err}
				}
			}(uuid))
		}
	}
	return tea.Batch(cmds...)
}

func (m changestreamModel) resumeCmd(modelUUID string) tea.Cmd {
	return func() tea.Msg {
		err := m.api.Resume(context.Background(), modelUUID)
		return resumeResultMsg{err: err}
	}
}

func (m changestreamModel) stepCmd(modelUUID string, count int) tea.Cmd {
	return func() tea.Msg {
		results, err := m.api.Step(context.Background(), modelUUID, count)
		return stepResultMsg{results: results, err: err}
	}
}

func (m changestreamModel) statusCmd() tea.Cmd {
	return func() tea.Msg {
		statuses, err := m.api.Status(context.Background())
		return statusResultMsg{statuses: statuses, err: err}
	}
}

func (m changestreamModel) Update(msg tea.Msg) (changestreamModel, tea.Cmd) {
	if m.pickerOpen {
		return m.updatePicker(msg)
	}
	if m.stepInputMode == stepInputActive {
		return m.updateStepInput(msg)
	}
	return m.updateNormal(msg)
}

func (m changestreamModel) updateStepInput(msg tea.Msg) (changestreamModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			count, err := strconv.Atoi(m.stepInput.Value())
			m.stepInputMode = stepInputNone
			m.stepInput.Blur()
			if err != nil || count < 1 {
				m.headerErr = "Invalid step count"
				return m, nil
			}
			ms := m.currentModelState()
			if ms != nil {
				return m, m.stepCmd(ms.UUID, count)
			}
			return m, nil
		case "esc":
			m.stepInputMode = stepInputNone
			m.stepInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.stepInput, cmd = m.stepInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m changestreamModel) updatePicker(msg tea.Msg) (changestreamModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.pickerCursor > 0 {
				m.pickerCursor--
			}
		case "down":
			if m.pickerCursor < len(m.pickerItems)-1 {
				m.pickerCursor++
			}
		case "enter":
			if m.pickerCursor >= 0 && m.pickerCursor < len(m.pickerItems) {
				selected := m.pickerItems[m.pickerCursor]
				m.pickerOpen = false
				m.currentModel = selected.UUID
				m.cursor = 0
				return m, func() tea.Msg {
					return switchModelMsg{modelUUID: selected.UUID, modelName: selected.Name}
				}
			}
		case "esc":
			m.pickerOpen = false
		}
	}
	return m, nil
}

func (m changestreamModel) updateNormal(msg tea.Msg) (changestreamModel, tea.Cmd) {
	switch msg := msg.(type) {
	case listModelsMsg:
		if msg.err == nil {
			m.mergeModels(msg.models)
			if msg.open && len(msg.models) > 0 {
				m.pickerOpen = true
				m.pickerItems = msg.models
				m.pickerCursor = 0
				for i, mi := range msg.models {
					if mi.UUID == m.currentModel {
						m.pickerCursor = i
						break
					}
				}
			}
		}
		return m, nil

	case statusResultMsg:
		if msg.err == nil {
			for _, s := range msg.statuses {
				uuid := s.Name
				if s.Name == "controller" && m.controllerModel != "" {
					uuid = m.controllerModel
				}
				if ms, ok := m.models[uuid]; ok {
					ms.Status = s.State
					ms.TxnID = s.TxnID
				}
			}
			cur := m.currentModelState()
			if cur != nil {
				cur.Paused = cur.Status == "PAUSED" || cur.Status == "STEP"
				if !cur.Paused {
					cur.PausedTxnIdx = -1
				}
			}
			if m.ready {
				m.viewport.SetContent(m.renderRows())
				m.syncViewportToCursor()
			}
		}
		return m, m.scheduleStatusPoll()

	case statusTickMsg:
		return m, m.statusCmd()

	case stepResultMsg:
		if msg.err != nil {
			m.headerErr = fmt.Sprintf("Step failed: %v", msg.err)
			return m, nil
		}
		m.headerErr = ""
		ms := m.currentModelState()
		if ms == nil {
			return m, nil
		}
		for i := len(msg.results) - 1; i >= 0; i-- {
			r := msg.results[i]
			txnMin := r.TxnMin
			txnMax := r.TxnMax
			if txnMax == 0 && r.EventCount == 0 {
				continue
			}
			if txnMin == txnMax {
				entry := transactionEntry{
					TxnID:      txnMin,
					EventCount: r.EventCount,
					TraceID:    r.TraceID,
					SpanID:     r.SpanID,
				}
				ms.Transactions = prependTransaction(ms.Transactions, entry)
			} else {
				entry := transactionEntry{
					TxnID:      txnMax,
					EventCount: r.EventCount,
					TraceID:    r.TraceID,
					SpanID:     r.SpanID,
				}
				ms.Transactions = prependTransaction(ms.Transactions, entry)
			}
		}
		if len(ms.Transactions) > 0 {
			m.cursor = 0
		}
		ms.Status = "PAUSED"
		ms.Paused = true
		ms.PausedTxnIdx = 0
		if m.ready {
			m.viewport.SetContent(m.renderRows())
			m.syncViewportToCursor()
		}
		if len(ms.Transactions) > 0 {
			return m, func() tea.Msg {
				return selectTxnMsg{txn: ms.Transactions[0]}
			}
		}
		return m, nil

	case pauseResultMsg:
		if msg.err != nil {
			m.headerErr = fmt.Sprintf("Pause failed: %v", msg.err)
			return m, nil
		}
		m.headerErr = ""
		return m, nil

	case resumeResultMsg:
		if msg.err != nil {
			m.headerErr = fmt.Sprintf("Resume failed: %v", msg.err)
			return m, nil
		}
		m.headerErr = ""
		ms := m.currentModelState()
		if ms != nil {
			ms.Paused = false
			ms.PausedTxnIdx = -1
			ms.Status = "RUNNING"
		}
		if m.ready {
			m.viewport.SetContent(m.renderRows())
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			if m.ready {
				m.syncViewportToCursor()
			}
		case "down":
			ms := m.currentModelState()
			if ms != nil && m.cursor < len(ms.Transactions)-1 {
				m.cursor++
			}
			if m.ready {
				m.syncViewportToCursor()
			}
		case "enter":
			ms := m.currentModelState()
			if ms != nil && m.cursor >= 0 && m.cursor < len(ms.Transactions) {
				selected := ms.Transactions[m.cursor]
				return m, func() tea.Msg {
					return selectTxnMsg{txn: selected}
				}
			}
		case "p":
			ms := m.currentModelState()
			if ms != nil && !ms.Paused {
				return m, m.pauseCmd(ms.UUID)
			}
		case "P":
			return m, m.pauseAllCmd()
		case "r":
			ms := m.currentModelState()
			if ms != nil && ms.Paused {
				return m, m.resumeCmd(ms.UUID)
			}
		case "s":
			ms := m.currentModelState()
			if ms != nil && ms.Paused {
				return m, m.stepCmd(ms.UUID, 1)
			}
		case "S":
			ms := m.currentModelState()
			if ms != nil && ms.Paused {
				m.stepInputMode = stepInputActive
				m.stepInput.SetValue("")
				m.stepInput.Focus()
				return m, nil
			}
		case "m":
			return m, m.fetchModelsForPickerCmd()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(m.Height(), 6)
		vpHeight := max(m.height-4, 1)
		m.viewport = viewport.New(msg.Width-4, vpHeight)
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

func prependTransaction(txs []transactionEntry, tx transactionEntry) []transactionEntry {
	txs = append([]transactionEntry{tx}, txs...)
	if len(txs) > maxTransactions {
		txs = txs[:maxTransactions]
	}
	return txs
}

func (m *changestreamModel) mergeModels(infos []ModelInfo) {
	for _, info := range infos {
		if info.IsController {
			m.controllerModel = info.UUID
		}
		if _, exists := m.models[info.UUID]; !exists {
			m.models[info.UUID] = &modelState{
				Name:         info.Name,
				UUID:         info.UUID,
				Status:       "RUNNING",
				Transactions: nil,
				PausedTxnIdx: -1,
			}
		}
	}
}

func (m *changestreamModel) clampCursor() {
	ms := m.currentModelState()
	if ms == nil || len(ms.Transactions) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(ms.Transactions) {
		m.cursor = len(ms.Transactions) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m changestreamModel) currentModelState() *modelState {
	return m.models[m.currentModel]
}

func (m changestreamModel) Height() int {
	if m.height < 6 {
		return 6
	}
	return m.height
}

func (m changestreamModel) renderRows() string {
	ms := m.currentModelState()
	if ms == nil {
		return ""
	}

	highlightStyle := lipgloss.NewStyle().Reverse(true)
	pausedDotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)

	var rows string
	for i, txn := range ms.Transactions {
		marker := "  "
		if ms.Paused && i == ms.PausedTxnIdx {
			marker = "● "
		}
		line := fmt.Sprintf("%s%d  %d events  trace: %s", marker, txn.TxnID, txn.EventCount, txn.TraceID)
		if i == m.cursor {
			line = highlightStyle.Render(line)
		} else if ms.Paused && i == ms.PausedTxnIdx {
			line = pausedDotStyle.Render(marker) + fmt.Sprintf("%d  %d events  trace: %s", txn.TxnID, txn.EventCount, txn.TraceID)
		}
		rows += line + "\n"
	}
	return rows
}

func (m *changestreamModel) syncViewportToCursor() {
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

func (m changestreamModel) View() string {
	if m.pickerOpen {
		return m.viewPicker()
	}
	return m.viewNormal()
}

func (m changestreamModel) viewNormal() string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	shortcutStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("228"))

	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	title := headerStyle.Render("Changestream")

	ms := m.currentModelState()

	var shortcuts string
	if m.stepInputMode == stepInputActive {
		shortcuts = shortcutStyle.Render(m.stepInput.View())
	} else if ms != nil && ms.Paused {
		shortcuts = shortcutStyle.Render("[s]tep [S]tepN [r]esume")
	} else {
		shortcuts = shortcutStyle.Render("[p]ause [P]ause all")
	}

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	statusText := "running"
	if ms != nil {
		switch ms.Status {
		case "PAUSED":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
			statusText = "paused"
		case "STEP":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
			statusText = "step"
		}
	}
	status := statusStyle.Render(statusText)

	modelLabel := ""
	if ms != nil {
		modelLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Render("  " + ms.Name)
	}

	headerLine := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", shortcuts, "  ", status, modelLabel)

	if m.headerErr != "" {
		headerLine += "\n" + errStyle.Render(m.headerErr)
	}

	var content string
	if m.ready {
		m.viewport.SetContent(m.renderRows())
		content = headerLine + "\n" + m.viewport.View()
	} else {
		content = headerLine + "\n" + m.renderRows()
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

	return borderStyle.Render(content)
}

func (m changestreamModel) viewPicker() string {
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

	highlightStyle := lipgloss.NewStyle().
		Reverse(true)

	statusStyle := map[string]lipgloss.Style{
		"RUNNING": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"PAUSED":  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		"STEP":    lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
	}

	content := titleStyle.Render("Select Model") + "\n\n"

	for i, item := range m.pickerItems {
		state := "RUNNING"
		if ms, ok := m.models[item.UUID]; ok {
			state = ms.Status
		}

		statusIndicator := ""
		if style, ok := statusStyle[state]; ok {
			statusIndicator = style.Render(fmt.Sprintf("[%s]", state))
		} else {
			statusIndicator = fmt.Sprintf("[%s]", state)
		}

		prefix := "  "
		if item.UUID == m.currentModel {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%s %s", prefix, item.Name, statusIndicator)
		if i == m.pickerCursor {
			line = highlightStyle.Render(line)
		}
		content += line + "\n"
	}

	content += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("Enter to select, Esc to cancel")

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		borderStyle.Render(content),
		lipgloss.WithWhitespaceChars(" "),
	)
}

func (m *changestreamModel) pausedModelUUIDs() []string {
	var uuids []string
	for uuid, ms := range m.models {
		if ms.Paused || ms.Status == "STEP" {
			uuids = append(uuids, uuid)
		}
	}
	return uuids
}
