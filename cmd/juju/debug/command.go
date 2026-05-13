// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/juju/gnuflag"
	"github.com/mattn/go-isatty"

	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/modelcmd"
)

const debugCommandDoc = `
Launch an interactive terminal UI for inspecting and controlling the
Juju changestream. The TUI presents three panes - changestream status
and controls, log tail, and trace inspection - and allows a developer
to pause, step through, and resume the changestream interactively.

Press m inside the TUI to switch between models. Press P to pause all
models, r to resume the current model, or q to quit (which resumes all
paused models).

This command requires a controller connection and controller superuser
access.
`

func NewDebugCommand() cmd.Command {
	return modelcmd.WrapController(&debugCommand{})
}

type debugCommand struct {
	modelcmd.ControllerCommandBase
}

func (c *debugCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "debug",
		Purpose: "Launch an interactive TUI for inspecting and controlling the Juju changestream.",
		Doc:     debugCommandDoc,
		SeeAlso: []string{"debug-log", "debug-hooks", "debug-code"},
	})
}

func (c *debugCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
}

func (c *debugCommand) Init(args []string) error {
	return cmd.CheckEmpty(args)
}

func (c *debugCommand) Run(ctx *cmd.Context) error {
	if !isTerminal(ctx.Stdout) {
		return fmt.Errorf("juju debug requires an interactive terminal")
	}

	controllerName, err := c.ControllerName()
	if err != nil {
		return fmt.Errorf("getting controller name: %w", err)
	}

	store := c.ClientStore()
	modelName, err := store.CurrentModel(controllerName)
	if err != nil {
		modelName = ""
	}

	apiRoot, err := c.NewAPIRoot(ctx)
	if err != nil {
		return fmt.Errorf("connecting to API: %w", err)
	}

	logAPI := newLogAPIClient(apiRoot)

	var modelUUID string
	if modelName != "" {
		uuids, err := c.ModelUUIDs(ctx, []string{modelName})
		if err == nil && len(uuids) > 0 {
			modelUUID = uuids[0]
		}
	}

	accountDetails, err := c.CurrentAccountDetails()
	if err != nil {
		return fmt.Errorf("getting account details: %w", err)
	}

	modelManagerClient, err := c.NewModelManagerAPIClient(ctx)
	if err != nil {
		return fmt.Errorf("creating model manager client: %w", err)
	}

	debugAPI := newMockDebugChangeStreamAPI()
	modelLister := newModelListAPIClient(modelManagerClient, accountDetails.User)
	defer modelManagerClient.Close()
	defer debugAPI.Close()

	qualifyingStore := modelcmd.QualifyingClientStore{ClientStore: store}
	switchModel := func(modelName string) error {
		return qualifyingStore.SetCurrentModel(controllerName, modelName)
	}

	model := newDebugModel(controllerName, modelName, modelUUID, logAPI, debugAPI, modelLister, switchModel)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI exited with error: %w", err)
	}
	return nil
}

func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(file.Fd())
}
