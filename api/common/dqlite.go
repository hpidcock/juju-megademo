// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/juju/errors"

	"github.com/juju/juju/api/base"
)

const protocolVersion = "v1"

type DqliteDatabase struct {
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
}

type DqliteObject struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type DqliteNode struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Role    string `json:"role"`
}

type DqliteQueryResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

type dqliteDDLResult struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

type dqliteRequest struct {
	Version   string `json:"version"`
	RequestID string `json:"request_id"`
	Type      string `json:"type"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	SQL       string `json:"sql,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type dqliteResponse struct {
	Version   string          `json:"version"`
	RequestID string          `json:"request_id"`
	Type      string          `json:"type"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

type DqliteClient struct {
	conn base.Stream
	mu   sync.Mutex
}

func OpenDqlite(ctx context.Context, src base.ControllerStreamConnector) (*DqliteClient, error) {
	stream, err := src.ConnectControllerStream(ctx, "/dqlite", nil, nil)
	if err != nil {
		return nil, errors.Trace(err)
	}
	client := &DqliteClient{conn: stream}
	if err := client.handshake(ctx); err != nil {
		stream.Close()
		return nil, errors.Trace(err)
	}
	return client, nil
}

func (c *DqliteClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func newRequestID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%08x", binary.BigEndian.Uint32(b[:]))
}

func (c *DqliteClient) handshake(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	handshake := dqliteRequest{Version: protocolVersion}
	if err := c.conn.WriteJSON(handshake); err != nil {
		return fmt.Errorf("version handshake write: %w", err)
	}

	var resp dqliteResponse
	if err := c.conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("version handshake read: %w", err)
	}

	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return fmt.Errorf("unsupported server version: %q", resp.Version)
	}
	return nil
}

func (c *DqliteClient) send(ctx context.Context, req dqliteRequest) (dqliteResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.WriteJSON(req); err != nil {
		return dqliteResponse{}, fmt.Errorf("write request: %w", err)
	}

	var resp dqliteResponse
	if err := c.conn.ReadJSON(&resp); err != nil {
		return dqliteResponse{}, fmt.Errorf("read response: %w", err)
	}
	return resp, nil
}

func (c *DqliteClient) Databases(ctx context.Context) ([]DqliteDatabase, error) {
	req := dqliteRequest{
		Version:   protocolVersion,
		RequestID: newRequestID(),
		Type:      "databases",
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return nil, errors.Errorf("unsupported server version: %q", resp.Version)
	}
	var databases []DqliteDatabase
	if err := json.Unmarshal(resp.Result, &databases); err != nil {
		return nil, fmt.Errorf("unmarshal databases: %w", err)
	}
	return databases, nil
}

func (c *DqliteClient) Objects(ctx context.Context, ns string, kind string) ([]DqliteObject, error) {
	req := dqliteRequest{
		Version:   protocolVersion,
		RequestID: newRequestID(),
		Type:      "objects",
		Namespace: ns,
		Kind:      kind,
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return nil, errors.Errorf("unsupported server version: %q", resp.Version)
	}
	var objects []DqliteObject
	if err := json.Unmarshal(resp.Result, &objects); err != nil {
		return nil, fmt.Errorf("unmarshal objects: %w", err)
	}
	return objects, nil
}

func (c *DqliteClient) DDL(ctx context.Context, ns string, name string) (string, error) {
	req := dqliteRequest{
		Version:   protocolVersion,
		RequestID: newRequestID(),
		Type:      "ddl",
		Namespace: ns,
		Name:      name,
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return "", errors.Trace(err)
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return "", errors.Errorf("unsupported server version: %q", resp.Version)
	}
	var result dqliteDDLResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("unmarshal ddl: %w", err)
	}
	return result.SQL, nil
}

func (c *DqliteClient) Query(ctx context.Context, ns string, sql string, limit int) (*DqliteQueryResult, error) {
	req := dqliteRequest{
		Version:   protocolVersion,
		RequestID: newRequestID(),
		Type:      "query",
		Namespace: ns,
		SQL:       sql,
		Limit:     limit,
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return nil, errors.Errorf("unsupported server version: %q", resp.Version)
	}
	var result DqliteQueryResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal query result: %w", err)
	}
	return &result, nil
}

func (c *DqliteClient) Cluster(ctx context.Context) ([]DqliteNode, error) {
	req := dqliteRequest{
		Version:   protocolVersion,
		RequestID: newRequestID(),
		Type:      "cluster",
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return nil, errors.Errorf("unsupported server version: %q", resp.Version)
	}
	var nodes []DqliteNode
	if err := json.Unmarshal(resp.Result, &nodes); err != nil {
		return nil, fmt.Errorf("unmarshal cluster: %w", err)
	}
	return nodes, nil
}
