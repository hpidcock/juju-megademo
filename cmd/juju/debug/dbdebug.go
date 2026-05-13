// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"os"

	"github.com/charmbracelet/bubbletea"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"
	"github.com/mattn/go-isatty"

	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/modelcmd"
)

type dbDebugCommand struct {
	modelcmd.ControllerCommandBase
	database string
	limit    int
}

func NewDbDebugCommand() cmd.Command {
	c := &dbDebugCommand{limit: 100}
	return modelcmd.WrapController(c, modelcmd.WrapControllerSkipControllerFlags)
}

var description = `
Launches a terminal UI for browsing and querying Juju Dqlite databases.`[1:]

func (c *dbDebugCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "db-debug",
		Purpose: "launch an interactive Dqlite database browser",
		Doc:     description,
	})
}

func (c *dbDebugCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	f.StringVar(&c.database, "database", "", "Pre-select target database (controller or model name)")
	f.IntVar(&c.limit, "limit", 100, "Default query row limit (1-1000)")
}

func (c *dbDebugCommand) Init(args []string) error {
	if c.limit < 1 || c.limit > 1000 {
		return errors.Errorf("--limit must be between 1 and 1000")
	}
	return cmd.CheckEmpty(args)
}

func (c *dbDebugCommand) Run(ctx *cmd.Context) error {
	f, ok := ctx.Stdout.(*os.File)
	if !ok || !isatty.IsTerminal(f.Fd()) {
		return errors.New("juju db-debug requires an interactive terminal")
	}

	api := &mockDqliteAPI{}
	model := NewDqliteModel(api)
	model.preSelectDatabase = c.database
	model.defaultLimit = c.limit

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type mockDqliteAPI struct{}

func (m *mockDqliteAPI) Databases(_ context.Context) ([]DqliteDatabase, error) {
	return nil, nil
}

func (m *mockDqliteAPI) Objects(_ context.Context, _, _ string) ([]DqliteObject, error) {
	return nil, nil
}

func (m *mockDqliteAPI) DDL(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *mockDqliteAPI) Query(_ context.Context, _, _ string, _ int) (*DqliteQueryResult, error) {
	return nil, nil
}

func (m *mockDqliteAPI) Cluster(_ context.Context) ([]DqliteNode, error) {
	return nil, nil
}