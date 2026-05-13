// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"bytes"
	stdtesting "testing"

	"github.com/juju/gnuflag"
	"github.com/juju/tc"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/internal/testing"
	"github.com/juju/juju/rpc/params"
)

type DebugChangeStreamSuite struct {
	testing.FakeJujuXDGDataHomeSuite
	store *jujuclient.MemStore
}

func TestDebugChangeStreamSuite(t *stdtesting.T) {
	tc.Run(t, &DebugChangeStreamSuite{})
}

func (s *DebugChangeStreamSuite) SetUpTest(c *tc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)

	s.store = jujuclient.NewMemStore()
	err := s.store.AddController("ctrl", jujuclient.ControllerDetails{
		ControllerUUID: "ctrl-uuid",
		CACert:         "cacert",
	})
	c.Assert(err, tc.ErrorIsNil)
	err = s.store.SetCurrentController("ctrl")
	c.Assert(err, tc.ErrorIsNil)
	err = s.store.UpdateAccount("ctrl", jujuclient.AccountDetails{
		User: "admin",
	})
	c.Assert(err, tc.ErrorIsNil)
	err = s.store.UpdateModel("ctrl", "admin/mymodel", jujuclient.ModelDetails{
		ModelUUID: "model-uuid",
		ModelType: model.IAAS,
	})
	c.Assert(err, tc.ErrorIsNil)
	err = s.store.SetCurrentModel("ctrl", "admin/mymodel")
	c.Assert(err, tc.ErrorIsNil)
}

// --- validateScopeFlags tests ---

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsAllAndControllerMutuallyExclusive(c *tc.C) {
	err := validateScopeFlags("", true, true)
	c.Check(err, tc.ErrorMatches, ".*mutually exclusive.*")
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsModelWithAll(c *tc.C) {
	err := validateScopeFlags("mymodel", true, false)
	c.Check(err, tc.ErrorMatches, ".*cannot be combined with --all.*")
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsModelWithController(c *tc.C) {
	err := validateScopeFlags("mymodel", false, true)
	c.Check(err, tc.ErrorMatches, ".*cannot be combined with --controller-db.*")
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsValidNoFlags(c *tc.C) {
	err := validateScopeFlags("", false, false)
	c.Check(err, tc.ErrorIsNil)
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsValidModelOnly(c *tc.C) {
	err := validateScopeFlags("mymodel", false, false)
	c.Check(err, tc.ErrorIsNil)
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsValidAllOnly(c *tc.C) {
	err := validateScopeFlags("", true, false)
	c.Check(err, tc.ErrorIsNil)
}

func (s *DebugChangeStreamSuite) TestValidateScopeFlagsValidControllerOnly(c *tc.C) {
	err := validateScopeFlags("", false, true)
	c.Check(err, tc.ErrorIsNil)
}

// --- resolveTarget tests ---

func (s *DebugChangeStreamSuite) TestResolveTargetAll(c *tc.C) {
	target, name, err := resolveTarget(
		c.Context(), s.newBase(c), "", true, false,
	)
	c.Check(err, tc.ErrorIsNil)
	c.Check(target, tc.DeepEquals, params.DebugChangeStreamTarget{All: true})
	c.Check(name, tc.Equals, "")
}

func (s *DebugChangeStreamSuite) TestResolveTargetController(c *tc.C) {
	target, name, err := resolveTarget(
		c.Context(), s.newBase(c), "", false, true,
	)
	c.Check(err, tc.ErrorIsNil)
	c.Check(target, tc.DeepEquals, params.DebugChangeStreamTarget{Controller: true})
	c.Check(name, tc.Equals, "")
}

// newBase returns a ControllerCommandBase wired up with the suite's
// store and controller name set. It marks runStarted so that
// ControllerName and ClientStore can be called outside of a Run.
func (s *DebugChangeStreamSuite) newBase(c *tc.C) modelcmd.ControllerCommandBase {
	var base modelcmd.ControllerCommandBase
	base.SetClientStore(s.store)
	err := base.SetControllerName("ctrl", false)
	c.Assert(err, tc.ErrorIsNil)
	return base
}

// --- Init tests ---

func (s *DebugChangeStreamSuite) TestPauseInitAllAndController(c *tc.C) {
	cmd := &debugPauseCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--all", "--controller-db"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*mutually exclusive.*")
}

func (s *DebugChangeStreamSuite) TestPauseInitModelWithAll(c *tc.C) {
	cmd := &debugPauseCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--model", "m", "--all"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*cannot be combined.*")
}

func (s *DebugChangeStreamSuite) TestPauseInitModelWithController(c *tc.C) {
	cmd := &debugPauseCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--model", "m", "--controller-db"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*cannot be combined.*")
}

func (s *DebugChangeStreamSuite) TestStepInitCountDefaultsToOne(c *tc.C) {
	cmd := &debugStepCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, nil)
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorIsNil)
	c.Check(cmd.count, tc.Equals, 1)
}

func (s *DebugChangeStreamSuite) TestStepInitCountZero(c *tc.C) {
	cmd := &debugStepCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--count", "0"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*at least 1.*")
}

func (s *DebugChangeStreamSuite) TestStepInitCountNegative(c *tc.C) {
	cmd := &debugStepCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--count", "-1"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*at least 1.*")
}

func (s *DebugChangeStreamSuite) TestResumeInitAllAndController(c *tc.C) {
	cmd := &debugResumeCommand{}
	cmd.SetClientStore(s.store)
	f := gnuflag.NewFlagSet("test", gnuflag.ContinueOnError)
	cmd.SetFlags(f)
	err := f.Parse(true, []string{"--all", "--controller-db"})
	c.Assert(err, tc.ErrorIsNil)
	err = cmd.Init(nil)
	c.Check(err, tc.ErrorMatches, ".*mutually exclusive.*")
}

// --- printStepResult tests ---

func (s *DebugChangeStreamSuite) TestPrintStepResultSingleDB(c *tc.C) {
	ctx := cmdtesting.Context(c)
	db := params.DebugChangeStreamDBResult{
		Name:       "model-uuid",
		TxnMin:     40,
		TxnMax:     42,
		EventCount: 3,
	}
	printStepResult(ctx, db.Name, db, false)
	c.Check(ctx.Stdout.(*bytes.Buffer).String(), tc.Equals,
		"Stepped 3 transaction(s) (txn 42): 3 event(s).\n",
	)
}

func (s *DebugChangeStreamSuite) TestPrintStepResultWithPrefixController(c *tc.C) {
	ctx := cmdtesting.Context(c)
	db := params.DebugChangeStreamDBResult{
		Name:       "controller",
		TxnMin:     18,
		TxnMax:     19,
		EventCount: 1,
	}
	printStepResult(ctx, db.Name, db, true)
	c.Check(ctx.Stdout.(*bytes.Buffer).String(), tc.Equals,
		"controller: stepped 2 transaction(s) (txn 19): 1 event(s).\n",
	)
}

func (s *DebugChangeStreamSuite) TestPrintStepResultWithPrefixModel(c *tc.C) {
	ctx := cmdtesting.Context(c)
	db := params.DebugChangeStreamDBResult{
		Name:       "model-uuid",
		TxnMin:     40,
		TxnMax:     42,
		EventCount: 3,
	}
	printStepResult(ctx, db.Name, db, true)
	c.Check(ctx.Stdout.(*bytes.Buffer).String(), tc.Equals,
		"model-uuid: stepped 3 transaction(s) (txn 42): 3 event(s).\n",
	)
}

func (s *DebugChangeStreamSuite) TestPrintStepResultAlreadyAtHeadNoPrefix(c *tc.C) {
	ctx := cmdtesting.Context(c)
	db := params.DebugChangeStreamDBResult{
		Name:       "model-uuid",
		TxnMin:     0,
		TxnMax:     0,
		EventCount: 0,
	}
	printStepResult(ctx, db.Name, db, false)
	c.Check(ctx.Stdout.(*bytes.Buffer).String(), tc.Equals,
		"Already at head, 0 event(s).\n",
	)
}

func (s *DebugChangeStreamSuite) TestPrintStepResultAlreadyAtHeadWithPrefix(c *tc.C) {
	ctx := cmdtesting.Context(c)
	db := params.DebugChangeStreamDBResult{
		Name:       "model-uuid",
		TxnMin:     0,
		TxnMax:     0,
		EventCount: 0,
	}
	printStepResult(ctx, db.Name, db, true)
	c.Check(ctx.Stdout.(*bytes.Buffer).String(), tc.Equals,
		"model-uuid: already at head, 0 event(s).\n",
	)
}
