// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/common"
	coretesting "github.com/juju/juju/internal/testing"
)

type DqliteClientSuite struct {
	coretesting.BaseSuite
}

func TestDqliteClientSuite(t *testing.T) {
	tc.Run(t, &DqliteClientSuite{})
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func handshakeResponse() []byte {
	return mustMarshal(map[string]string{"version": "v1"})
}

func commonResponseWithResult(requestType string, result json.RawMessage) []byte {
	return mustMarshal(map[string]any{
		"version":    "v1",
		"request_id": "req1",
		"type":       requestType,
		"result":     result,
	})
}

// mockStream implements base.Stream for testing.
type mockStream struct {
	responses  chan []byte
	readErr    error
	writeErr   error
	written    []any
	mu         sync.Mutex
	closed     bool
	closeCount int
}

func (s *mockStream) WriteJSON(v any) error {
	s.mu.Lock()
	s.written = append(s.written, v)
	s.mu.Unlock()
	return s.writeErr
}

func (s *mockStream) ReadJSON(v any) error {
	if s.readErr != nil {
		return s.readErr
	}
	select {
	case raw, ok := <-s.responses:
		if !ok {
			return io.EOF
		}
		return json.Unmarshal(raw, v)
	}
}

func (s *mockStream) NextReader() (int, io.Reader, error) {
	return 0, nil, nil
}

func (s *mockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCount++
	s.closed = true
	return nil
}

func (s *mockStream) getWritten() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]any, len(s.written))
	copy(result, s.written)
	return result
}

// mockConnector implements base.ControllerStreamConnector for testing.
type mockConnector struct {
	responses chan []byte
	stream    *mockStream
}

func (c *mockConnector) ConnectControllerStream(_ context.Context, path string, attrs url.Values, headers http.Header) (base.Stream, error) {
	if c.stream == nil {
		c.stream = &mockStream{
			responses: c.responses,
		}
	}
	return c.stream, nil
}

func newMockConnector(responses ...[]byte) *mockConnector {
	ch := make(chan []byte, len(responses))
	for _, r := range responses {
		ch <- r
	}
	return &mockConnector{responses: ch}
}

func (s *DqliteClientSuite) TestOpenDqliteHandshakeSucceeds(c *tc.C) {
	connector := newMockConnector(handshakeResponse())
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	written := connector.stream.getWritten()
	c.Assert(written, tc.HasLen, 1)
}

func (s *DqliteClientSuite) TestOpenDqliteHandshakeError(c *tc.C) {
	errResp := mustMarshal(map[string]string{"version": "v1", "error": "access denied"})
	connector := newMockConnector(errResp)
	_, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorMatches, "access denied")
}

func (s *DqliteClientSuite) TestOpenDqliteHandshakeVersionMismatch(c *tc.C) {
	mismatchResp := mustMarshal(map[string]string{"version": "v2"})
	connector := newMockConnector(mismatchResp)
	_, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorMatches, `unsupported server version: "v2"`)
}

func (s *DqliteClientSuite) TestOpenDqliteHandshakeReadError(c *tc.C) {
	connector := &mockConnector{}
	connector.stream = &mockStream{
		responses: make(chan []byte, 1),
		readErr:   io.ErrUnexpectedEOF,
	}
	_, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorMatches, "version handshake read: unexpected EOF")
}

func (s *DqliteClientSuite) TestNewRequestIDProducesDifferentValues(c *tc.C) {
	id1 := common.NewRequestID()
	id2 := common.NewRequestID()
	c.Check(id1, tc.Not(tc.Equals), id2)
	c.Check(id1, tc.HasLen, 8)
	c.Check(id2, tc.HasLen, 8)
}

func (s *DqliteClientSuite) TestDatabases(c *tc.C) {
	databases := []common.DqliteDatabase{
		{Name: "controller", Namespace: "controller", Type: "controller"},
		{Name: "lxd-pilot", UUID: "deadbeef", Namespace: "deadbeef", Type: "model"},
	}
	databasesJSON := mustMarshal(databases)
	resp := commonResponseWithResult("databases", databasesJSON)
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	result, err := client.Databases(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.DeepEquals, databases)

	written := connector.stream.getWritten()
	c.Check(written, tc.HasLen, 2)
}

func (s *DqliteClientSuite) TestObjects(c *tc.C) {
	objects := []common.DqliteObject{
		{Name: "change_log", Kind: "table"},
		{Name: "v_model_status", Kind: "view"},
	}
	resp := commonResponseWithResult("objects", mustMarshal(objects))
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	result, err := client.Objects(c.Context(), "ns1", "table")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.DeepEquals, objects)
}

func (s *DqliteClientSuite) TestDDL(c *tc.C) {
	ddlResult := mustMarshal(map[string]string{
		"name": "change_log",
		"sql":  "CREATE TABLE change_log (id INTEGER PRIMARY KEY)",
	})
	resp := commonResponseWithResult("ddl", ddlResult)
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	result, err := client.DDL(c.Context(), "ns1", "change_log")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.Equals, "CREATE TABLE change_log (id INTEGER PRIMARY KEY)")
}

func (s *DqliteClientSuite) TestQuery(c *tc.C) {
	queryResult := &common.DqliteQueryResult{
		Columns:   []string{"id", "name"},
		Rows:      [][]string{{"1", "foo"}, {"2", "bar"}},
		RowCount:  2,
		Truncated: true,
	}
	resp := commonResponseWithResult("query", mustMarshal(queryResult))
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	result, err := client.Query(c.Context(), "ns1", "SELECT * FROM t", 100)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result.Columns, tc.DeepEquals, []string{"id", "name"})
	c.Check(result.Rows, tc.DeepEquals, [][]string{{"1", "foo"}, {"2", "bar"}})
	c.Check(result.RowCount, tc.Equals, 2)
	c.Check(result.Truncated, tc.IsTrue)
}

func (s *DqliteClientSuite) TestCluster(c *tc.C) {
	nodes := []common.DqliteNode{
		{ID: "00ab", Address: "10.0.0.1:12345", Role: "voter"},
		{ID: "00cd", Address: "10.0.0.2:12345", Role: "stand-by"},
	}
	resp := commonResponseWithResult("cluster", mustMarshal(nodes))
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	result, err := client.Cluster(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.DeepEquals, nodes)
}

func (s *DqliteClientSuite) TestServerError(c *tc.C) {
	errResp := mustMarshal(map[string]string{"version": "v1", "error": "query timeout"})
	connector := newMockConnector(handshakeResponse(), errResp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Query(c.Context(), "ns1", "SELECT 1", 10)
	c.Assert(err, tc.ErrorMatches, "query timeout")
}

func (s *DqliteClientSuite) TestObjectsServerError(c *tc.C) {
	errResp := mustMarshal(map[string]string{"version": "v1", "error": "server error"})
	connector := newMockConnector(handshakeResponse(), errResp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Objects(c.Context(), "ns1", "table")
	c.Assert(err, tc.ErrorMatches, "server error")
}

func (s *DqliteClientSuite) TestDDLServerError(c *tc.C) {
	errResp := mustMarshal(map[string]string{"version": "v1", "error": "not found"})
	connector := newMockConnector(handshakeResponse(), errResp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.DDL(c.Context(), "ns1", "nonexistent")
	c.Assert(err, tc.ErrorMatches, "not found")
}

func (s *DqliteClientSuite) TestClusterServerError(c *tc.C) {
	errResp := mustMarshal(map[string]string{"version": "v1", "error": "unavailable"})
	connector := newMockConnector(handshakeResponse(), errResp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Cluster(c.Context())
	c.Assert(err, tc.ErrorMatches, "unavailable")
}

func (s *DqliteClientSuite) TestVersionMismatchOnResponse(c *tc.C) {
	mismatchResp := mustMarshal(map[string]string{"version": "v99"})
	connector := newMockConnector(handshakeResponse(), mismatchResp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Databases(c.Context())
	c.Assert(err, tc.ErrorMatches, `unsupported server version: "v99"`)
}

func (s *DqliteClientSuite) TestWriteError(c *tc.C) {
	connector := &mockConnector{}
	connector.stream = &mockStream{
		responses: make(chan []byte, 1),
	}
	connector.stream.responses <- handshakeResponse()
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	connector.stream.writeErr = io.ErrClosedPipe
	_, err = client.Databases(c.Context())
	c.Assert(err, tc.ErrorMatches, ".*closed pipe.*")
}

func (s *DqliteClientSuite) TestReadError(c *tc.C) {
	connector := &mockConnector{}
	connector.stream = &mockStream{
		responses: make(chan []byte, 1),
	}
	connector.stream.responses <- handshakeResponse()
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	connector.stream.readErr = io.ErrClosedPipe
	_, err = client.Databases(c.Context())
	c.Assert(err, tc.ErrorMatches, ".*closed pipe.*")
}

func (s *DqliteClientSuite) TestConcurrentSends(c *tc.C) {
	dbResp := commonResponseWithResult("databases", mustMarshal([]common.DqliteDatabase{
		{Name: "controller", Namespace: "controller", Type: "controller"},
	}))
	dbResp2 := commonResponseWithResult("databases", mustMarshal([]common.DqliteDatabase{
		{Name: "lxd-pilot", UUID: "deadbeef", Namespace: "deadbeef", Type: "model"},
	}))
	connector := newMockConnector(handshakeResponse(), dbResp, dbResp2)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var result1, result2 []common.DqliteDatabase
	var dbErr1, dbErr2 error

	go func() {
		defer wg.Done()
		result1, dbErr1 = client.Databases(c.Context())
	}()
	go func() {
		defer wg.Done()
		result2, dbErr2 = client.Databases(c.Context())
	}()
	wg.Wait()

	c.Assert(dbErr1, tc.ErrorIsNil)
	c.Check(result1, tc.HasLen, 1)
	c.Assert(dbErr2, tc.ErrorIsNil)
	c.Check(result2, tc.HasLen, 1)
}

func (s *DqliteClientSuite) TestClose(c *tc.C) {
	connector := newMockConnector(handshakeResponse())
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)

	err = client.Close()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(connector.stream.closeCount, tc.Equals, 1)
}

func (s *DqliteClientSuite) TestDoubleClose(c *tc.C) {
	connector := newMockConnector(handshakeResponse())
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)

	err = client.Close()
	c.Assert(err, tc.ErrorIsNil)
	err = client.Close()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(connector.stream.closeCount, tc.Equals, 2)
}

func (s *DqliteClientSuite) TestResponseTypeMismatch(c *tc.C) {
	databases := mustMarshal([]common.DqliteDatabase{
		{Name: "controller", Namespace: "controller", Type: "controller"},
	})
	resp := commonResponseWithResult("cluster", databases)
	connector := newMockConnector(handshakeResponse(), resp)
	client, err := common.OpenDqlite(c.Context(), connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Databases(c.Context())
	c.Assert(err, tc.ErrorMatches, `unexpected response type: "cluster" \(expected "databases"\)`)
}

func (s *DqliteClientSuite) TestContextCancellation(c *tc.C) {
	ctx, cancel := context.WithCancel(c.Context())
	cancel()

	connector := newMockConnector(handshakeResponse())
	client, err := common.OpenDqlite(ctx, connector)
	c.Assert(err, tc.ErrorIsNil)
	defer client.Close()

	_, err = client.Databases(ctx)
	c.Assert(err, tc.ErrorMatches, "context error: context canceled")
}