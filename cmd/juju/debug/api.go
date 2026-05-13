// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/common"
)

type LogAPI interface {
	StreamLogs(ctx context.Context, params common.DebugLogParams) (<-chan common.LogMessage, error)
	Close() error
}

type logAPIClient struct {
	conn api.Connection
}

func newLogAPIClient(conn api.Connection) *logAPIClient {
	return &logAPIClient{conn: conn}
}

func (c *logAPIClient) StreamLogs(ctx context.Context, params common.DebugLogParams) (<-chan common.LogMessage, error) {
	return common.StreamDebugLog(ctx, c.conn, params)
}

func (c *logAPIClient) Close() error {
	return c.conn.Close()
}
