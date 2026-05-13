// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type transactionEntry struct {
	TxnID      int64
	EventCount int
	TraceID    string
	SpanID     string
}

type changestreamModel struct {
	width         int
	height        int
	transactions  []transactionEntry
	cursor        int
	paused        bool
	pausedTxnIdx  int
	nextTxnID     int64
}

const maxTransactions = 10

const changestreamTickInterval = 500 * time.Millisecond

func newChangestreamModel() changestreamModel {
	m := changestreamModel{
		nextTxnID:    100,
		pausedTxnIdx: -1,
	}
	// TODO(phase-04): Replace mock transaction generation with real data from
	// the DebugChangeStream facade.
	m.transactions = generateMockTransactions(m.nextTxnID)
	m.nextTxnID += int64(len(m.transactions))
	return m
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
			TraceID:    randomHex(12),
			SpanID:     randomHex(12),
		}
	}
	return txns
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.IntN(len(hexChars))]
	}
	return string(b)
}

func (m changestreamModel) Init() tea.Cmd {
	return tickChangestream
}

func tickChangestream() tea.Msg {
	return changestreamTickMsg(time.Now())
}

func (m changestreamModel) Update(msg tea.Msg) (changestreamModel, tea.Cmd) {
	switch msg := msg.(type) {
	case changestreamTickMsg:
		if !m.paused {
			// TODO(phase-04): Replace mock refresh with real facade Status call.
			newTxns := generateMockTransactions(m.nextTxnID)
			m.nextTxnID += int64(len(newTxns))
			m.transactions = newTxns
			m.clampCursor()
		}
		return m, tickChangestream

	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.transactions)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.transactions) {
				selectedIdx := m.cursor
				return m, func() tea.Msg {
					return selectTxnMsg{txnIndex: selectedIdx}
				}
			}
		case "p":
			// TODO(phase-04): Call DebugChangeStream.Pause facade method.
			m.paused = true
			m.pausedTxnIdx = 0
		case "r":
			// TODO(phase-04): Call DebugChangeStream.Resume facade method.
			m.paused = false
			m.pausedTxnIdx = -1
		case "s":
			// TODO(phase-04): Call DebugChangeStream.Step facade method.
			if m.paused && m.pausedTxnIdx >= 0 && m.pausedTxnIdx < len(m.transactions)-1 {
				m.pausedTxnIdx++
			}
		case "S":
			// TODO(phase-04): Implement step-N with prompt, calling
			// DebugChangeStream.Step facade method with count.
		case "P":
			// TODO(phase-03): Implement pause-all across all model
			// changestreams via DebugChangeStream.Pause facade method.
		}
	}
	return m, nil
}

func (m *changestreamModel) clampCursor() {
	if len(m.transactions) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.transactions) {
		m.cursor = len(m.transactions) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m changestreamModel) View() string {
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
	var shortcuts string
	if m.paused {
		shortcuts = shortcutStyle.Render("[s]tep [r]esume")
	} else {
		shortcuts = shortcutStyle.Render("[p]ause [P]ause all")
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	statusText := "running"
	if m.paused {
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		statusText = "paused"
	}
	status := statusStyle.Render(statusText)
	headerLine := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", shortcuts, "  ", status)

	rows := ""
	for i, txn := range m.transactions {
		marker := "  "
		if m.paused && i == m.pausedTxnIdx {
			marker = "● "
		}
		line := fmt.Sprintf("%s%d  %d events  trace: %s", marker, txn.TxnID, txn.EventCount, txn.TraceID)
		if i == m.cursor {
			line = highlightStyle.Render(line)
		} else if m.paused && i == m.pausedTxnIdx {
			line = pausedDotStyle.Render(marker) + fmt.Sprintf("%d  %d events  trace: %s", txn.TxnID, txn.EventCount, txn.TraceID)
		}
		rows += line + "\n"
	}

	content := headerLine + "\n" + rows

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingLeft(1).
		PaddingRight(1).
		Width(m.width - 2).
		Height(m.height - 2)

	return borderStyle.Render(content)
}
