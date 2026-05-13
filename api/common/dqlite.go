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

// DqliteDatabase describes one selectable database.
type DqliteDatabase struct {
	// Name is "controller" or the model name.
	Name string `json:"name"`
	// UUID is empty for the controller and set for model databases.
	UUID string `json:"uuid"`
	// Namespace is the database namespace used internally.
	Namespace string `json:"namespace"`
	// Type is "controller" or "model".
	Type string `json:"type"`
}

// DqliteObject describes a table, view, or trigger.
type DqliteObject struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// DqliteNode describes a Dqlite cluster node.
type DqliteNode struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Role    string `json:"role"`
}

// DqliteQueryResult holds the result of a read-only query.
type DqliteQueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

// dqliteDDLResult holds the DDL response from the server.
type dqliteDDLResult struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

// dqliteRequest is the JSON structure sent to the /dqlite websocket.
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

// dqliteResponse is the JSON structure received from the /dqlite websocket.
type dqliteResponse struct {
	Version   string          `json:"version"`
	RequestID string          `json:"request_id"`
	Type      string          `json:"type"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// DqliteClient provides typed access to the /dqlite websocket.
type DqliteClient struct {
	conn base.Stream
	mu   sync.Mutex
}

// OpenDqlite dials the /dqlite websocket, performs a version handshake,
// and returns a client. The caller must already be logged in to the
// controller API. The caller is responsible for calling Close when done.
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

// Close closes the underlying websocket stream.
func (c *DqliteClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func newRequestID() string {
	return NewRequestID()
}

// NewRequestID generates a random 8-character hex request identifier.
func NewRequestID() string {
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
	if err := ctx.Err(); err != nil {
		return dqliteResponse{}, fmt.Errorf("context error: %w", err)
	}
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

// validateResponse checks the server response for protocol errors.
func (c *DqliteClient) validateResponse(resp dqliteResponse, expectType string) error {
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if resp.Version != protocolVersion {
		return fmt.Errorf("unsupported server version: %q", resp.Version)
	}
	if resp.Type != expectType {
		return fmt.Errorf("unexpected response type: %q (expected %q)", resp.Type, expectType)
	}
	return nil
}

// Databases returns the controller database and all model databases.
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
	if err := c.validateResponse(resp, "databases"); err != nil {
		return nil, err
	}
	var databases []DqliteDatabase
	if err := json.Unmarshal(resp.Result, &databases); err != nil {
		return nil, fmt.Errorf("unmarshal databases: %w", err)
	}
	return databases, nil
}

// Objects returns tables, views, or triggers in the given namespace.
// kind is "table", "view", or "trigger".
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
	if err := c.validateResponse(resp, "objects"); err != nil {
		return nil, err
	}
	var objects []DqliteObject
	if err := json.Unmarshal(resp.Result, &objects); err != nil {
		return nil, fmt.Errorf("unmarshal objects: %w", err)
	}
	return objects, nil
}

// DDL returns the CREATE statement for a table, view, or trigger.
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
	if err := c.validateResponse(resp, "ddl"); err != nil {
		return "", err
	}
	var result dqliteDDLResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("unmarshal ddl: %w", err)
	}
	return result.SQL, nil
}

// Query executes a bounded read-only query and returns formatted results.
// limit is clamped by the server to a maximum of 1000.
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
	if err := c.validateResponse(resp, "query"); err != nil {
		return nil, err
	}
	var result DqliteQueryResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal query result: %w", err)
	}
	return &result, nil
}

// Cluster returns Dqlite cluster node information.
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
	if err := c.validateResponse(resp, "cluster"); err != nil {
		return nil, err
	}
	var nodes []DqliteNode
	if err := json.Unmarshal(resp.Result, &nodes); err != nil {
		return nil, fmt.Errorf("unmarshal cluster: %w", err)
	}
	return nodes, nil
}
