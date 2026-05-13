// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/juju/loggo/v3"

	"github.com/juju/juju/api/common"
)

var logLevelColors = map[string]lipgloss.Style{
	"TRACE":    lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
	"DEBUG":    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	"INFO":     lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
	"WARNING":  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
	"ERROR":    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	"CRITICAL": lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1")),
}

type logModel struct {
	width  int
	height int
	active bool

	logAPI        LogAPI
	records       []common.LogMessage
	logCh         <-chan common.LogMessage
	streamCancel  context.CancelFunc
	streamVersion int
	disconnected  bool
	streamErr     string

	viewport viewport.Model
	ready    bool

	levelIndex   int
	moduleFilter string
	filtering    bool
	filterInput  textinput.Model
}

var logLevels = []struct {
	Name  string
	Level loggo.Level
}{
	{"TRACE", loggo.TRACE},
	{"DEBUG", loggo.DEBUG},
	{"INFO", loggo.INFO},
	{"WARNING", loggo.WARNING},
	{"ERROR", loggo.ERROR},
}

func newLogModel(logAPI LogAPI) logModel {
	ti := textinput.New()
	ti.Placeholder = "module filter"
	ti.CharLimit = 50

	return logModel{
		logAPI:      logAPI,
		levelIndex:  2,
		filterInput: ti,
	}
}

func (m logModel) Init() tea.Cmd {
	return startLogStreamCmd(m.logAPI, m.levelIndex, m.streamVersion)
}

func startLogStreamCmd(logAPI LogAPI, levelIndex int, version int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		params := common.DebugLogParams{
			Level: logLevels[levelIndex].Level,
		}
		ch, err := logAPI.StreamLogs(ctx, params)
		if err != nil {
			cancel()
			return logStreamReadyMsg{version: version, err: err}
		}
		return logStreamReadyMsg{ch: ch, cancel: cancel, version: version}
	}
}

func waitForLogMsg(ch <-chan common.LogMessage, version int) tea.Cmd {
	return func() tea.Msg {
		record, ok := <-ch
		if !ok {
			return logStreamDoneMsg{version: version}
		}
		return logMsg{record: record, version: version}
	}
}

func (m logModel) Update(msg tea.Msg) (logModel, tea.Cmd) {
	switch msg := msg.(type) {
	case logStreamReadyMsg:
		if msg.version == m.streamVersion {
			if msg.err != nil {
				m.streamErr = msg.err.Error()
			} else {
				if m.streamCancel != nil {
					m.streamCancel()
				}
				m.streamCancel = msg.cancel
				m.logCh = msg.ch
				m.disconnected = false
				m.streamErr = ""
				cmd := waitForLogMsg(msg.ch, m.streamVersion)
				return m, cmd
			}
		} else {
			if msg.cancel != nil {
				msg.cancel()
			}
		}
		return m, nil

	case logMsg:
		if msg.version != m.streamVersion {
			return m, nil
		}
		m.records = append(m.records, msg.record)
		const maxLogRecords = 50
		if len(m.records) > maxLogRecords {
			m.records = m.records[len(m.records)-maxLogRecords:]
		}
		if m.ready {
			m.viewport.SetContent(m.renderFilteredContent())
			if m.shouldDisplay(msg.record) {
				m.viewport.GotoBottom()
			}
		}
		return m, waitForLogMsg(m.logCh, m.streamVersion)

	case logStreamDoneMsg:
		if msg.version == m.streamVersion {
			m.disconnected = true
			if m.ready {
				m.viewport.SetContent(m.renderFilteredContent())
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "enter":
				m.moduleFilter = m.filterInput.Value()
				m.filtering = false
				m.filterInput.Blur()
				if m.ready {
					m.viewport.SetContent(m.renderFilteredContent())
				}
				return m, nil
			case "esc":
				m.filterInput.SetValue(m.moduleFilter)
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				return m, cmd
			}
		}
		switch msg.String() {
		case "l":
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.levelIndex = (m.levelIndex + 1) % len(logLevels)
			m.streamVersion++
			m.records = nil
			m.disconnected = false
			m.streamErr = ""
			return m, startLogStreamCmd(m.logAPI, m.levelIndex, m.streamVersion)
		case "/":
			m.filtering = true
			m.filterInput.SetValue(m.moduleFilter)
			m.filterInput.Focus()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = m.Height()
		m.viewport = viewport.New(msg.Width-4, max(m.Height()-3, 1))
		m.viewport.SetContent(m.renderFilteredContent())
		m.ready = true
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m logModel) shouldDisplay(record common.LogMessage) bool {
	if m.moduleFilter == "" {
		return true
	}
	return strings.Contains(
		strings.ToLower(record.Module),
		strings.ToLower(m.moduleFilter),
	)
}

func (m logModel) Height() int {
	if m.height < 4 {
		return 4
	}
	return m.height
}

func (m logModel) renderFilteredContent() string {
	var b strings.Builder
	if m.disconnected {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Log stream disconnected."))
		b.WriteString("\n")
	}
	for _, record := range m.records {
		if m.shouldDisplay(record) {
			b.WriteString(formatLogRecord(record))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func formatLogRecord(record common.LogMessage) string {
	ts := record.Timestamp.Format("15:04:05")
	level := record.Severity
	style, ok := logLevelColors[level]
	if !ok {
		style = lipgloss.NewStyle()
	}
	styledLevel := style.Render(fmt.Sprintf("%-5s", level))
	return fmt.Sprintf("%s %s %s %s", ts, styledLevel, record.Module, record.Message)
}

func (m logModel) View() string {
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

	titleText := fmt.Sprintf("Log [%s]", logLevels[m.levelIndex].Name)
	if m.moduleFilter != "" || m.filtering {
		titleText += fmt.Sprintf(" filter: %s", m.filterInput.View())
	}
	title := titleStyle.Render(titleText)

	var content string
	if m.streamErr != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		content = title + "\n" + errStyle.Render(m.streamErr)
	} else if !m.ready {
		content = title
	} else {
		content = title + "\n" + m.viewport.View()
	}

	return borderStyle.Render(content)
}
