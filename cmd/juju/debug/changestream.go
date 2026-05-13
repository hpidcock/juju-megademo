// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

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

type changestreamModel struct {
	width        int
	height       int
	active       bool
	currentModel string
	models       map[string]*modelState
	cursor       int

	pickerOpen   bool
	pickerItems  []ModelInfo
	pickerCursor int

	api         DebugChangeStreamAPI
	modelLister ModelListAPI
}

const maxTransactions = 10

const changestreamTickInterval = 500 * time.Millisecond

func newChangestreamModel(api DebugChangeStreamAPI, modelLister ModelListAPI, initialModelName, initialModelUUID string) changestreamModel {
	models := map[string]*modelState{
		initialModelUUID: {
			Name:         initialModelName,
			UUID:         initialModelUUID,
			Status:       "RUNNING",
			Transactions: generateMockTransactions(100),
			PausedTxnIdx: -1,
		},
	}
	return changestreamModel{
		active:       true,
		currentModel: initialModelUUID,
		models:       models,
		api:          api,
		modelLister:  modelLister,
	}
}

// TODO(phase-04): Replace mock transaction generation with real data from
// the DebugChangeStream facade.
func generateMockTransactions(baseID int64) []transactionEntry {
	count := rand.IntN(7) + 4
	txns := make([]transactionEntry, count)
	for i := range count {
		txnID := baseID + int64(count-1-i)
		txns[i] = transactionEntry{
			TxnID:      txnID,
			EventCount: rand.IntN(5) + 1,
			TraceID:    randomMockTraceID(),
			SpanID:     randomHex(8),
		}
	}
	return txns
}

var mockTraceIDs = []string{
	"f0250316350d16f308b71ab93cbf7510",
	"c23d861742e1815509564a7d176d3590",
	"6aa4a72c364edbd109cbf40d3520c1ae",
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.IntN(len(hexChars))]
	}
	return string(b)
}

func randomMockTraceID() string {
	if len(mockTraceIDs) > 0 {
		return mockTraceIDs[rand.IntN(len(mockTraceIDs))]
	}
	return randomHex(12)
}

func (m changestreamModel) Init() tea.Cmd {
	return tea.Batch(
		tickChangestream,
		m.scheduleStatusPoll(),
		m.fetchModelsCmd(),
	)
}

func tickChangestream() tea.Msg {
	return changestreamTickMsg(time.Now())
}

func scheduleChangestreamTick() tea.Cmd {
	return tea.Tick(changestreamTickInterval, func(t time.Time) tea.Msg {
		return changestreamTickMsg(t)
	})
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

func (m changestreamModel) Update(msg tea.Msg) (changestreamModel, tea.Cmd) {
	if m.pickerOpen {
		return m.updatePicker(msg)
	}
	return m.updateNormal(msg)
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

	case changestreamTickMsg:
		ms := m.currentModelState()
		if ms != nil && !ms.Paused {
			// TODO(phase-04): Replace mock refresh with real facade Status call.
			ms.Transactions = generateMockTransactions(ms.TxnID + int64(rand.IntN(20)+1))
			m.clampCursor()
		}
		return m, scheduleChangestreamTick()

	case statusTickMsg:
		// TODO(phase-04): Call api.Status(ctx) and update model states.
		return m, m.scheduleStatusPoll()

	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			ms := m.currentModelState()
			if ms != nil && m.cursor < len(ms.Transactions)-1 {
				m.cursor++
			}
		case "enter":
			ms := m.currentModelState()
			if ms != nil && m.cursor >= 0 && m.cursor < len(ms.Transactions) {
				selectedIdx := m.cursor
				return m, func() tea.Msg {
					return selectTxnMsg{txnIndex: selectedIdx}
				}
			}
		case "p":
			ms := m.currentModelState()
			if ms != nil && !ms.Paused {
				// TODO(phase-04): Call DebugChangeStream.Pause facade method.
				ms.Paused = true
				ms.PausedTxnIdx = 0
				ms.Status = "PAUSED"
			}
		case "P":
			// TODO(phase-04): Call DebugChangeStream.Pause for all models.
			for _, ms := range m.models {
				if !ms.Paused {
					ms.Paused = true
					ms.PausedTxnIdx = 0
					ms.Status = "PAUSED"
				}
			}
		case "r":
			ms := m.currentModelState()
			if ms != nil && ms.Paused {
				// TODO(phase-04): Call DebugChangeStream.Resume facade method.
				ms.Paused = false
				ms.PausedTxnIdx = -1
				ms.Status = "RUNNING"
			}
		case "s":
			ms := m.currentModelState()
			if ms != nil && ms.Paused && ms.PausedTxnIdx >= 0 && ms.PausedTxnIdx < len(ms.Transactions)-1 {
				// TODO(phase-04): Call DebugChangeStream.Step facade method.
				ms.PausedTxnIdx++
				ms.Status = "STEP"
			}
		case "S":
			// TODO(phase-04): Implement step-N with prompt.
		case "m":
			return m, m.fetchModelsForPickerCmd()
		}
	}

	return m, nil
}

func (m *changestreamModel) mergeModels(infos []ModelInfo) {
	for _, info := range infos {
		if _, exists := m.models[info.UUID]; !exists {
			m.models[info.UUID] = &modelState{
				Name:         info.Name,
				UUID:         info.UUID,
				Status:       "RUNNING",
				Transactions: generateMockTransactions(100),
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

	highlightStyle := lipgloss.NewStyle().
		Reverse(true)

	pausedDotStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		Bold(true)

	title := headerStyle.Render("Changestream")

	ms := m.currentModelState()

	var shortcuts string
	if ms != nil && ms.Paused {
		shortcuts = shortcutStyle.Render("[s]tep [r]esume")
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

	rows := ""
	if ms != nil {
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
	}

	content := headerLine + "\n" + rows

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
		Height(m.height - 2)

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
		if ms.Paused {
			uuids = append(uuids, uuid)
		}
	}
	return uuids
}
