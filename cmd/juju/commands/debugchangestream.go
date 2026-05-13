// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/gnuflag"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/client/debugchangestream"
	"github.com/juju/juju/api/jujuclient"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/rpc/params"
)

// shared scope flags and validation

func addScopeFlags(
	f *gnuflag.FlagSet,
	model *string,
	all *bool,
	controllerDB *bool,
) {
	f.StringVar(model, "m", "", "Target model (defaults to current model)")
	f.StringVar(model, "model", "", "")
	f.BoolVar(all, "all", false, "Target all databases (controller and all models)")
	f.BoolVar(controllerDB, "controller-db", false, "Target the controller database only")
}

func validateScopeFlags(
	model string, all, controllerDB bool,
) error {
	if all && controllerDB {
		return errors.New("--all and --controller-db are mutually exclusive")
	}
	if model != "" && all {
		return errors.New("--model cannot be combined with --all")
	}
	if model != "" && controllerDB {
		return errors.New("--model cannot be combined with --controller-db")
	}
	return nil
}

func resolveTarget(
	ctx context.Context,
	c modelcmd.ControllerCommandBase,
	model string,
	all, controllerDB bool,
) (params.DebugChangeStreamTarget, string, error) {
	if all {
		return params.DebugChangeStreamTarget{All: true}, "", nil
	}
	if controllerDB {
		return params.DebugChangeStreamTarget{Controller: true}, "", nil
	}
	modelName := model
	if modelName == "" {
		controllerName, err := c.ControllerName()
		if err != nil {
			return params.DebugChangeStreamTarget{}, "",
				errors.Trace(err)
		}
		modelName, err = c.ClientStore().CurrentModel(controllerName)
		if err != nil {
			return params.DebugChangeStreamTarget{}, "",
				errors.Annotate(err, "determining current model")
		}
	}
	uuids, err := c.ModelUUIDs(ctx, []string{modelName})
	if err != nil {
		return params.DebugChangeStreamTarget{}, "",
			errors.Trace(err)
	}
	return params.DebugChangeStreamTarget{
		ModelUUID: uuids[0],
	}, modelName, nil
}

// debugPauseCommand

var debugPauseSummary = `
Pauses the change stream for a model or controller.`[1:]

var debugPauseDetails = `

Pausing the change stream stops the controller from processing change
events for the targeted database(s). This is useful for debugging
purposes to inspect state at a specific point in time.

Use --controller-db to pause the controller database change stream,
--model to pause a specific model, or --all to pause all databases.

Examples:
    juju debug-pause
    juju debug-pause --model mymodel
    juju debug-pause --controller-db
    juju debug-pause --all
`[1:]

type debugPauseCommand struct {
	modelcmd.ControllerCommandBase

	model        string
	all          bool
	controllerDB bool

	newClient func(base.APICallCloser) *debugchangestream.Client
}

func newDebugPauseCommand(store jujuclient.ClientStore) cmd.Command {
	command := &debugPauseCommand{
		newClient: func(caller base.APICallCloser) *debugchangestream.Client {
			return debugchangestream.NewClient(caller)
		},
	}
	command.SetClientStore(store)
	return modelcmd.WrapController(command)
}

func (c *debugPauseCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "debug-pause",
		Purpose: debugPauseSummary,
		Doc:     debugPauseDetails,
		SeeAlso: []string{"debug-step", "debug-resume"},
	})
}

func (c *debugPauseCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	addScopeFlags(f, &c.model, &c.all, &c.controllerDB)
}

func (c *debugPauseCommand) Init(args []string) error {
	return validateScopeFlags(c.model, c.all, c.controllerDB)
}

func (c *debugPauseCommand) Run(ctx *cmd.Context) error {
	root, err := c.NewAPIRoot(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	defer root.Close()

	target, modelName, err := resolveTarget(
		ctx.Context, c.ControllerCommandBase,
		c.model, c.all, c.controllerDB,
	)
	if err != nil {
		return errors.Trace(err)
	}

	client := c.newClient(root)
	result, err := client.Pause(ctx.Context, target)
	if err != nil {
		return errors.Trace(err)
	}

	if target.All {
		fmt.Fprintf(ctx.Stdout, "Change stream paused (all).\n")
		return nil
	}
	for _, db := range result.Results {
		if db.Error != nil {
			fmt.Fprintf(ctx.Stderr, "error: %s: %s\n", db.Name, db.Error)
			continue
		}
		if target.Controller {
			fmt.Fprintf(ctx.Stdout, "Change stream paused (controller, txn %d).\n", db.TxnMax)
		} else {
			fmt.Fprintf(ctx.Stdout, "Change stream paused (model %q, txn %d).\n", modelName, db.TxnMax)
		}
	}
	return nil
}

// debugStepCommand

var debugStepSummary = `
Steps the change stream forward by one or more transactions.`[1:]

var debugStepDetails = `

Stepping the change stream advances processing by the specified number
of transactions, making the corresponding change events visible to
watchers.

Use --controller-db to step the controller database, --model to step a
specific model, or --all to step all databases.

Examples:
    juju debug-step
    juju debug-step --count 5
    juju debug-step --model mymodel
    juju debug-step --controller-db --count 3
    juju debug-step --all
`[1:]

type debugStepCommand struct {
	modelcmd.ControllerCommandBase

	model        string
	all          bool
	controllerDB bool
	count        int

	newClient func(base.APICallCloser) *debugchangestream.Client
}

func newDebugStepCommand(store jujuclient.ClientStore) cmd.Command {
	command := &debugStepCommand{
		count: 1,
		newClient: func(caller base.APICallCloser) *debugchangestream.Client {
			return debugchangestream.NewClient(caller)
		},
	}
	command.SetClientStore(store)
	return modelcmd.WrapController(command)
}

func (c *debugStepCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "debug-step",
		Purpose: debugStepSummary,
		Doc:     debugStepDetails,
		SeeAlso: []string{"debug-pause", "debug-resume"},
	})
}

func (c *debugStepCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	addScopeFlags(f, &c.model, &c.all, &c.controllerDB)
	f.IntVar(&c.count, "count", 1, "Number of transactions to step")
}

func (c *debugStepCommand) Init(args []string) error {
	if c.count < 1 {
		return errors.New("--count must be at least 1")
	}
	return validateScopeFlags(c.model, c.all, c.controllerDB)
}

func (c *debugStepCommand) Run(ctx *cmd.Context) error {
	root, err := c.NewAPIRoot(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	defer root.Close()

	target, _, err := resolveTarget(
		ctx.Context, c.ControllerCommandBase,
		c.model, c.all, c.controllerDB,
	)
	if err != nil {
		return errors.Trace(err)
	}

	client := c.newClient(root)
	result, err := client.Step(ctx.Context, target, c.count)
	if err != nil {
		return errors.Trace(err)
	}

	for _, db := range result.Results {
		if db.Error != nil {
			fmt.Fprintf(ctx.Stderr, "error: %s: %s\n", db.Name, db.Error)
			continue
		}
		if target.All {
			printStepResult(ctx, db.Name, db, true)
		} else {
			printStepResult(ctx, db.Name, db, false)
		}
	}
	return nil
}

func printStepResult(
	ctx *cmd.Context,
	name string,
	db params.DebugChangeStreamDBResult,
	prefix bool,
) {
	if db.TxnMin == 0 && db.EventCount == 0 {
		if prefix {
			fmt.Fprintf(ctx.Stdout, "%s: already at head, 0 event(s).\n", name)
		} else {
			fmt.Fprintf(ctx.Stdout, "Already at head, 0 event(s).\n")
		}
		return
	}
	if prefix {
		fmt.Fprintf(
			ctx.Stdout,
			"%s: stepped %d transaction(s) (txn %d): %d event(s).\n",
			name, db.TxnMax-db.TxnMin+1, db.TxnMax, db.EventCount,
		)
	} else {
		fmt.Fprintf(
			ctx.Stdout,
			"Stepped %d transaction(s) (txn %d): %d event(s).\n",
			db.TxnMax-db.TxnMin+1, db.TxnMax, db.EventCount,
		)
	}
}

// debugResumeCommand

var debugResumeSummary = `
Resumes the change stream for a model or controller.`[1:]

var debugResumeDetails = `

Resuming the change stream restarts processing of change events for
the targeted database(s) after a pause.

Use --controller-db to resume the controller database, --model to
resume a specific model, or --all to resume all databases.

Examples:
    juju debug-resume
    juju debug-resume --model mymodel
    juju debug-resume --controller-db
    juju debug-resume --all
`[1:]

type debugResumeCommand struct {
	modelcmd.ControllerCommandBase

	model        string
	all          bool
	controllerDB bool

	newClient func(base.APICallCloser) *debugchangestream.Client
}

func newDebugResumeCommand(store jujuclient.ClientStore) cmd.Command {
	command := &debugResumeCommand{
		newClient: func(caller base.APICallCloser) *debugchangestream.Client {
			return debugchangestream.NewClient(caller)
		},
	}
	command.SetClientStore(store)
	return modelcmd.WrapController(command)
}

func (c *debugResumeCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "debug-resume",
		Purpose: debugResumeSummary,
		Doc:     debugResumeDetails,
		SeeAlso: []string{"debug-pause", "debug-step"},
	})
}

func (c *debugResumeCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	addScopeFlags(f, &c.model, &c.all, &c.controllerDB)
}

func (c *debugResumeCommand) Init(args []string) error {
	return validateScopeFlags(c.model, c.all, c.controllerDB)
}

func (c *debugResumeCommand) Run(ctx *cmd.Context) error {
	root, err := c.NewAPIRoot(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	defer root.Close()

	target, modelName, err := resolveTarget(
		ctx.Context, c.ControllerCommandBase,
		c.model, c.all, c.controllerDB,
	)
	if err != nil {
		return errors.Trace(err)
	}

	client := c.newClient(root)
	err = client.Resume(ctx.Context, target)
	if err != nil {
		return errors.Trace(err)
	}

	if target.All {
		fmt.Fprintf(ctx.Stdout, "Change stream resumed (all).\n")
	} else if target.Controller {
		fmt.Fprintf(ctx.Stdout, "Change stream resumed (controller).\n")
	} else {
		fmt.Fprintf(ctx.Stdout, "Change stream resumed (model %q).\n", modelName)
	}
	return nil
}
