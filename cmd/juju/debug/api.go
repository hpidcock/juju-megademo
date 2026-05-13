// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/client/modelmanager"
	"github.com/juju/juju/api/common"
)

type StreamStatus struct {
	Name  string
	State string
	TxnID int64
}

type DebugChangeStreamAPI interface {
	Status(ctx context.Context) ([]StreamStatus, error)
	Pause(ctx context.Context, modelUUID string) error
	Resume(ctx context.Context, modelUUID string) error
	Close() error
}

type ModelInfo struct {
	Name         string
	UUID         string
	IsController bool
}

type ModelListAPI interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
	Close() error
}

type modelListAPIClient struct {
	client *modelmanager.Client
	user   string
	all    bool
}

func newModelListAPIClient(client *modelmanager.Client, user string) *modelListAPIClient {
	return &modelListAPIClient{client: client, user: user, all: true}
}

func (c *modelListAPIClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	summaries, err := c.client.ListModelSummaries(ctx, c.user, c.all)
	if err != nil {
		return nil, err
	}
	result := make([]ModelInfo, 0, len(summaries))
	for _, s := range summaries {
		if s.Error != nil {
			continue
		}
		result = append(result, ModelInfo{
			Name:         s.Name,
			UUID:         s.UUID,
			IsController: s.IsController,
		})
	}
	return result, nil
}

func (c *modelListAPIClient) Close() error {
	return c.client.Close()
}

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
