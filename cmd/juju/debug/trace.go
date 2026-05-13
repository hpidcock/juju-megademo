// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type spanEntry struct {
	Operation string
	Duration  string
}

type traceModel struct {
	width       int
	height      int
	active      bool
	entries     []spanEntry
	selectedTxn *transactionEntry
}

func newTraceModel() traceModel {
	return traceModel{}
}

func (m traceModel) Init() tea.Cmd {
	return nil
}

func (m traceModel) Update(msg tea.Msg) (traceModel, tea.Cmd) {
	switch msg.(type) {
	case selectTxnMsg:
	}
	return m, nil
}

func (m *traceModel) setTransaction(txn transactionEntry) {
	m.selectedTxn = &txn
	// TODO(phase-02): Replace hardcoded mock spans with real spans fetched
	// from Grafana Tempo for the selected transaction's trace ID.
	if txn.TxnID == 42 {
		m.entries = []spanEntry{
			{Operation: "juju:API.AddRelation", Duration: "1.2ms"},
			{Operation: "juju:changestream.write", Duration: "0.3ms"},
			{Operation: "juju:worker.uniter", Duration: "0.8ms"},
		}
	} else {
		m.entries = nil
	}
}

func (m traceModel) View() string {
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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	title := titleStyle.Render("Trace")

	var content string
	if m.selectedTxn == nil {
		content = title + "\n" + "Select a transaction to view traces."
	} else {
		var b strings.Builder
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("txn %d — trace %s — span %s",
			m.selectedTxn.TxnID,
			m.selectedTxn.TraceID,
			m.selectedTxn.SpanID,
		))
		b.WriteString("\n")
		for _, entry := range m.entries {
			b.WriteString(fmt.Sprintf("  → %-30s %s\n", entry.Operation, entry.Duration))
		}
		content = b.String()
	}

	return borderStyle.Render(content)
}
