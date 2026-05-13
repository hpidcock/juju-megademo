// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type traceModel struct {
	width       int
	height      int
	active      bool
	tempoAPI    TempoAPI
	traceCache  map[string]*TraceData
	spinner     spinner.Model
	spinning    bool
	fetching    string
	fetchErr    string
	selectedTxn *transactionEntry
	viewport    viewport.Model
	ready       bool
}

func newTraceModel(tempoAPI TempoAPI) traceModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	return traceModel{
		tempoAPI:   tempoAPI,
		traceCache: make(map[string]*TraceData),
		spinner:    s,
	}
}

func (m traceModel) Init() tea.Cmd {
	if m.spinning {
		return m.spinner.Tick
	}
	return nil
}

func (m traceModel) Update(msg tea.Msg) (traceModel, tea.Cmd) {
	switch msg := msg.(type) {
	case selectTxnMsg:
		return m, nil
	case fetchTraceResultMsg:
		if msg.traceID == m.fetching {
			m.fetching = ""
			m.spinning = false
			if msg.err != nil {
				m.fetchErr = msg.err.Error()
			} else {
				m.fetchErr = ""
				m.traceCache[msg.traceID] = msg.data
			}
			if m.ready {
		m.viewport.SetContent(m.renderSpans())
				m.viewport.GotoTop()
			}
		}
		return m, nil
	case spinner.TickMsg:
		if m.spinning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case tea.KeyMsg:
		if m.active && m.ready {
			switch msg.String() {
			case "up":
				m.viewport.LineUp(1)
			case "down":
				m.viewport.LineDown(1)
			default:
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(m.Height(), 6)
		vpHeight := max(m.height-4, 1)
		m.viewport = viewport.New(msg.Width-4, vpHeight)
		m.viewport.SetContent(m.renderSpans())
		m.ready = true
	}
	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *traceModel) setTransaction(txn transactionEntry) {
	m.selectedTxn = &txn
	m.fetchErr = ""

	if m.ready {
		m.viewport.SetContent(m.renderSpans())
		m.viewport.GotoTop()
	}

	if txn.TraceID == "" {
		return
	}

	if _, cached := m.traceCache[txn.TraceID]; cached {
		return
	}

	if m.tempoAPI == nil || !m.tempoAPI.IsConfigured() {
		return
	}

	m.fetching = txn.TraceID
	m.spinning = true
}

func fetchTraceCmd(tempoAPI TempoAPI, traceID string) tea.Cmd {
	return func() tea.Msg {
		data, err := tempoAPI.FetchTrace(context.Background(), traceID)
		return fetchTraceResultMsg{traceID: traceID, data: data, err: err}
	}
}

func (m traceModel) Height() int {
	if m.height < 6 {
		return 6
	}
	return m.height
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
		Height(m.Height() - 2)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	header := titleStyle.Render("Trace")
	if m.selectedTxn != nil {
		header += fmt.Sprintf("  txn %d — trace %s — span %s",
			m.selectedTxn.TxnID,
			m.selectedTxn.TraceID,
			m.selectedTxn.SpanID,
		)
	}

	var content string
	if !m.ready {
		if m.selectedTxn == nil {
			content = header + "\n" + "Select a transaction to view traces."
		} else {
			content = header + "\n" + m.renderSpans()
		}
	} else {
		m.viewport.SetContent(m.renderSpans())
		content = header + "\n" + m.viewport.View()
	}

	return borderStyle.Render(content)
}

func (m traceModel) renderSpans() string {
	if m.selectedTxn == nil {
		return "Select a transaction to view traces."
	}

	var b strings.Builder

	if m.tempoAPI == nil || !m.tempoAPI.IsConfigured() {
		b.WriteString(notConfiguredMessage())
	} else if m.spinning && m.fetching == m.selectedTxn.TraceID {
		b.WriteString(m.spinner.View() + " fetching trace…")
	} else if m.fetchErr != "" && m.fetching == "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		b.WriteString(errStyle.Render(m.fetchErr))
	} else if cached, ok := m.traceCache[m.selectedTxn.TraceID]; ok {
		if len(cached.Spans) == 0 {
			b.WriteString("  no spans found")
		} else {
			depthMap := buildDepthMap(cached.Spans)
			serviceStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
			prevService := ""
			for _, span := range cached.Spans {
				depth := depthMap[span.SpanID]
				indent := strings.Repeat("  ", depth)
				service := ""
				if span.Service != "" && span.Service != prevService {
					service = serviceStyle.Render(span.Service) + " "
					prevService = span.Service
				}
				b.WriteString(fmt.Sprintf("%s→ %s%s  %s\n",
					indent, service, span.Operation, span.Duration))
			}
		}
	} else if m.selectedTxn.TraceID != "" {
		b.WriteString(notConfiguredMessage())
	}

	return b.String()
}

func notConfiguredMessage() string {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	return warnStyle.Render(
		"Tempo not configured. Set open-telemetry-endpoint in controller config.",
	)
}

func buildDepthMap(spans []SpanEntry) map[string]int {
	depthMap := make(map[string]int)
	spanByID := make(map[string]SpanEntry)
	for _, s := range spans {
		spanByID[s.SpanID] = s
	}
	var resolveDepth func(string) int
	resolveDepth = func(id string) int {
		if d, ok := depthMap[id]; ok {
			return d
		}
		s, found := spanByID[id]
		if !found || s.ParentID == "" {
			depthMap[id] = 0
			return 0
		}
		d := resolveDepth(s.ParentID) + 1
		depthMap[id] = d
		return d
	}
	for _, s := range spans {
		resolveDepth(s.SpanID)
	}
	return depthMap
}
