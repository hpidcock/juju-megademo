// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debugchangestream

import (
	"context"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/rpc/params"
)

// Option is a function that can be used to configure a Client.
type Option = base.Option

// WithTracer returns an Option that configures the Client to use the
// supplied tracer.
var WithTracer = base.WithTracer

// Client provides access to the DebugChangeStream API facade.
type Client struct {
	base.ClientFacade
	facade base.FacadeCaller
}

// NewClient returns a new DebugChangeStream client.
func NewClient(caller base.APICallCloser, options ...Option) *Client {
	frontend, backend := base.NewClientFacade(
		caller, "DebugChangeStream", options...,
	)
	return &Client{
		ClientFacade: frontend,
		facade:       backend,
	}
}

// Pause pauses the change stream for the given target.
func (c *Client) Pause(
	ctx context.Context,
	target params.DebugChangeStreamTarget,
) (params.DebugChangeStreamPauseResult, error) {
	var result params.DebugChangeStreamPauseResult
	args := params.DebugChangeStreamArgs{Target: target}
	err := c.facade.FacadeCall(ctx, "Pause", args, &result)
	return result, err
}

// Step steps the change stream forward by count transactions for the
// given target.
func (c *Client) Step(
	ctx context.Context,
	target params.DebugChangeStreamTarget,
	count int,
) (params.DebugChangeStreamStepResult, error) {
	var result params.DebugChangeStreamStepResult
	args := params.DebugChangeStreamStepArgs{
		Target: target,
		Count:  count,
	}
	err := c.facade.FacadeCall(ctx, "Step", args, &result)
	return result, err
}

// Resume resumes the change stream for the given target.
func (c *Client) Resume(
	ctx context.Context,
	target params.DebugChangeStreamTarget,
) error {
	args := params.DebugChangeStreamArgs{Target: target}
	return c.facade.FacadeCall(ctx, "Resume", args, nil)
}
