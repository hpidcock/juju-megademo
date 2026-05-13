// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debugchangestream

import (
	"context"

	"github.com/juju/juju/api/base"
)

type Option = base.Option

var WithTracer = base.WithTracer

type Client struct {
	base.ClientFacade
	facade base.FacadeCaller
}

func NewClient(caller base.APICallCloser, options ...Option) *Client {
	frontend, backend := base.NewClientFacade(caller, "DebugChangeStream", options...)
	return &Client{ClientFacade: frontend, facade: backend}
}

func (c *Client) Status(ctx context.Context) ([]StatusResult, error) {
	var results StatusResults
	err := c.facade.FacadeCall(ctx, "Status", nil, &results)
	if err != nil {
		return nil, err
	}
	return results.Results, nil
}

func (c *Client) Pause(ctx context.Context, modelUUID string) error {
	return c.facade.FacadeCall(ctx, "Pause", modelUUID, nil)
}

func (c *Client) Resume(ctx context.Context, modelUUID string) error {
	return c.facade.FacadeCall(ctx, "Resume", modelUUID, nil)
}

type StatusResult struct {
	Name  string
	State string
	TxnID int64
}

type StatusResults struct {
	Results []StatusResult
}
