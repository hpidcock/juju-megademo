// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"strings"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/client/debugchangestream"
	"github.com/juju/juju/api/client/modelmanager"
	apicontroller "github.com/juju/juju/api/controller/controller"
	"github.com/juju/juju/api/common"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/rpc/params"
)

type TempoAPI interface {
	FetchTrace(ctx context.Context, traceID string) (*TraceData, error)
	IsConfigured() bool
}

type TraceData struct {
	TraceID string
	Spans   []SpanEntry
}

type SpanEntry struct {
	SpanID      string
	Operation   string
	Service     string
	Duration    string
	ParentID    string
	startNano   int64
}

type ControllerConfigAPI interface {
	ControllerConfig(ctx context.Context) (controller.Config, error)
	Close() error
}

type controllerConfigAPIClient struct {
	client *apicontroller.Client
}

func newControllerConfigAPIClient(client *apicontroller.Client) *controllerConfigAPIClient {
	return &controllerConfigAPIClient{client: client}
}

func (c *controllerConfigAPIClient) ControllerConfig(ctx context.Context) (controller.Config, error) {
	return c.client.ControllerConfig(ctx)
}

func (c *controllerConfigAPIClient) Close() error {
	return c.client.Close()
}

type StreamStatus struct {
	Name  string
	State string
	TxnID int64
}

type StepResult struct {
	TxnMin     int64
	TxnMax     int64
	EventCount int
	TraceID    string
	SpanID     string
}

type DebugChangeStreamAPI interface {
	Status(ctx context.Context) ([]StreamStatus, error)
	Pause(ctx context.Context, modelUUID string) error
	Step(ctx context.Context, modelUUID string, count int) ([]StepResult, error)
	Resume(ctx context.Context, modelUUID string) error
	Close() error
}

type debugChangeStreamAPIClient struct {
	client *debugchangestream.Client
}

func newDebugChangeStreamAPIClient(caller base.APICallCloser) *debugChangeStreamAPIClient {
	return &debugChangeStreamAPIClient{
		client: debugchangestream.NewClient(caller),
	}
}

func (c *debugChangeStreamAPIClient) Status(ctx context.Context) ([]StreamStatus, error) {
	result, err := c.client.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StreamStatus, 0, len(result.Results))
	for _, r := range result.Results {
		status := StreamStatus{
			Name:  r.Name,
			State: strings.ToUpper(r.State),
			TxnID: r.TxnID,
		}
		if r.Error != nil {
			continue
		}
		out = append(out, status)
	}
	return out, nil
}

func (c *debugChangeStreamAPIClient) Pause(ctx context.Context, modelUUID string) error {
	_, err := c.client.Pause(ctx, params.DebugChangeStreamTarget{
		ModelUUID: modelUUID,
	})
	return err
}

func (c *debugChangeStreamAPIClient) Step(ctx context.Context, modelUUID string, count int) ([]StepResult, error) {
	result, err := c.client.Step(ctx, params.DebugChangeStreamTarget{
		ModelUUID: modelUUID,
	}, count)
	if err != nil {
		return nil, err
	}
	var out []StepResult
	for _, r := range result.Results {
		if r.Error != nil {
			continue
		}
		out = append(out, StepResult{
			TxnMin:     r.TxnMin,
			TxnMax:     r.TxnMax,
			EventCount: r.EventCount,
			TraceID:    r.TraceID,
			SpanID:     r.SpanID,
		})
	}
	return out, nil
}

func (c *debugChangeStreamAPIClient) Resume(ctx context.Context, modelUUID string) error {
	return c.client.Resume(ctx, params.DebugChangeStreamTarget{
		ModelUUID: modelUUID,
	})
}

func (c *debugChangeStreamAPIClient) Close() error {
	return c.client.Close()
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
