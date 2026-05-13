// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"bytes"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/internal/testing"
)

type dbDebugSuite struct {
	testing.BaseSuite
}

func TestDbDebugSuite(t *stdtesting.T) {
	tc.Run(t, &dbDebugSuite{})
}

func (s *dbDebugSuite) TestInfoName(c *tc.C) {
	d := newDbDebugCommand()
	info := d.Info()
	c.Check(info.Name, tc.Equals, "db-debug")
}

func (s *dbDebugSuite) TestInitDefaultLimitValid(c *tc.C) {
	d := newDbDebugCommand()
	err := d.Init(nil)
	c.Check(err, tc.ErrorIsNil)
}

func (s *dbDebugSuite) TestInitLimitZero(c *tc.C) {
	d := newDbDebugCommand()
	d.limit = 0
	err := d.Init(nil)
	c.Check(err, tc.ErrorMatches, "--limit must be between 1 and 1000")
}

func (s *dbDebugSuite) TestInitLimit1001(c *tc.C) {
	d := newDbDebugCommand()
	d.limit = 1001
	err := d.Init(nil)
	c.Check(err, tc.ErrorMatches, "--limit must be between 1 and 1000")
}

func (s *dbDebugSuite) TestInitLimitBoundaryMin(c *tc.C) {
	d := newDbDebugCommand()
	d.limit = 1
	err := d.Init(nil)
	c.Check(err, tc.ErrorIsNil)
}

func (s *dbDebugSuite) TestInitLimitBoundaryMax(c *tc.C) {
	d := newDbDebugCommand()
	d.limit = 1000
	err := d.Init(nil)
	c.Check(err, tc.ErrorIsNil)
}

func (s *dbDebugSuite) TestInitExtraArgs(c *tc.C) {
	d := newDbDebugCommand()
	err := d.Init([]string{"extra"})
	c.Check(err, tc.ErrorMatches, `unrecognized args: \["extra"\]`)
}

func (s *dbDebugSuite) TestRunNonTTY(c *tc.C) {
	d := newDbDebugCommand()
	ctx := &cmd.Context{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	err := d.Run(ctx)
	c.Check(err, tc.ErrorMatches, "juju db-debug requires an interactive terminal")
}

func newDbDebugCommand() *dbDebugCommand {
	return &dbDebugCommand{limit: 100}
}