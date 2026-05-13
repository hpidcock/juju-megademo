// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var logLevelColors = map[string]lipgloss.Style{
	"TRACE":   lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
	"DEBUG":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	"INFO":    lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
	"WARNING": lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
	"ERROR":   lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
}

type logModel struct {
	width      int
	height     int
	viewport   viewport.Model
	lines      []string
	levelIndex int
	ready      bool
}

var logLevels = []string{"TRACE", "DEBUG", "INFO", "WARNING", "ERROR"}

func newLogModel() logModel {
	// TODO(phase-01): Replace mock log lines with live stream from the
	// Logger facade via WatchLoggerAPI.
	mockLines := []string{
		"10:42:01 INFO  juju.worker.uniter handling configure",
		"10:42:01 DEBUG juju.changestream dispatching term 42",
		"10:42:00 INFO  juju.worker.caas starting",
		"10:41:59 WARN  juju.api reconnecting",
		"10:41:58 ERROR juju.db connection lost",
	}
	return logModel{
		lines:      mockLines,
		levelIndex: 2,
	}
}

func (m logModel) Init() tea.Cmd {
	return nil
}

func (m logModel) Update(msg tea.Msg) (logModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "l":
			m.levelIndex = (m.levelIndex + 1) % len(logLevels)
		// TODO(phase-01): Implement "/" for module filter text input.
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = m.Height()
		m.viewport = viewport.New(msg.Width-2, m.Height()-2)
		m.viewport.SetContent(m.renderLines())
		m.ready = true
	}
	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m logModel) Height() int {
	if m.height < 4 {
		return 4
	}
	return m.height
}

func (m logModel) renderLines() string {
	var b strings.Builder
	for _, line := range m.lines {
		b.WriteString(colorizeLogLine(line))
		b.WriteString("\n")
	}
	return b.String()
}

func colorizeLogLine(line string) string {
	for level, style := range logLevelColors {
		if strings.Contains(line, " "+level+" ") || strings.Contains(line, " "+level+"  ") {
			parts := strings.SplitN(line, level, 2)
			return parts[0] + style.Render(level) + parts[1]
		}
	}
	return line
}

func (m logModel) View() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingLeft(1).
		PaddingRight(1).
		Width(m.width - 2).
		Height(m.height - 2)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	title := titleStyle.Render(fmt.Sprintf("Log (level: %s)", logLevels[m.levelIndex]))

	var content string
	if !m.ready {
		content = title + "\n" + m.renderLines()
	} else {
		content = title + "\n" + m.viewport.View()
	}

	return borderStyle.Render(content)
}
