// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"bytes"
	"context"
	"errors"
	stdtesting "testing"

	tea "github.com/charmbracelet/bubbletea"
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

// ---------------------------------------------------------------------------
// Command tests (Task 2)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Mock API
// ---------------------------------------------------------------------------

type recordedMockAPI struct {
	databases    []DqliteDatabase
	databasesErr error

	objects    []DqliteObject
	objectsErr error
	objectsCalls []struct {
		ns   string
		kind string
	}

	ddl     string
	ddlErr  error
	ddlCalls []struct {
		ns   string
		name string
	}

	queryResult *DqliteQueryResult
	queryErr    error
	queryCalls  []struct {
		ns    string
		sql   string
		limit int
	}

	clusterNodes []DqliteNode
	clusterErr   error
	clusterCalls int

	databasesCalls int
}

func (a *recordedMockAPI) Databases(_ context.Context) ([]DqliteDatabase, error) {
	a.databasesCalls++
	return a.databases, a.databasesErr
}

func (a *recordedMockAPI) Objects(_ context.Context, ns, kind string) ([]DqliteObject, error) {
	a.objectsCalls = append(a.objectsCalls, struct {
		ns   string
		kind string
	}{ns, kind})
	return a.objects, a.objectsErr
}

func (a *recordedMockAPI) DDL(_ context.Context, ns, name string) (string, error) {
	a.ddlCalls = append(a.ddlCalls, struct {
		ns   string
		name string
	}{ns, name})
	return a.ddl, a.ddlErr
}

func (a *recordedMockAPI) Query(_ context.Context, ns, sql string, limit int) (*DqliteQueryResult, error) {
	a.queryCalls = append(a.queryCalls, struct {
		ns    string
		sql   string
		limit int
	}{ns, sql, limit})
	return a.queryResult, a.queryErr
}

func (a *recordedMockAPI) Cluster(_ context.Context) ([]DqliteNode, error) {
	a.clusterCalls++
	return a.clusterNodes, a.clusterErr
}

// step runs model.Update and returns the model re-typed to *dqliteModel.
func step(m *dqliteModel, msg tea.Msg) (*dqliteModel, tea.Cmd) {
	n, cmd := m.Update(msg)
	return n.(*dqliteModel), cmd
}

// execCmd calls the Cmd and returns the result message.
func execCmd(cmd tea.Cmd) tea.Msg {
	return cmd()
}

// ---------------------------------------------------------------------------
// Init & hardcoded message handler tests
// ---------------------------------------------------------------------------

func (s *dbDebugSuite) TestInit(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	cmd := m.Init()
	c.Check(cmd, tc.NotNil)
}

func (s *dbDebugSuite) TestHardcodedAllMsgPopulates(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	msg := hardcodedAllMsg{
		databases:    []DqliteDatabase{{Name: "controller", Namespace: "controller", Type: "controller"}},
		objects:      []DqliteObject{{Name: "change_log", Kind: "table"}},
		ddl:          "CREATE TABLE t (id INTEGER)",
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1:12345", Role: "voter"}},
		queryResult: &DqliteQueryResult{
			Columns:   []string{"col1"},
			Rows:      [][]string{{"val1"}},
			Count:     1,
			Truncated: false,
		},
	}
	m, _ = step(m, msg)
	c.Check(len(m.databases), tc.Equals, 1)
	c.Check(len(m.objects), tc.Equals, 1)
	c.Check(m.ddl, tc.Equals, "CREATE TABLE t (id INTEGER)")
	c.Check(len(m.clusterNodes), tc.Equals, 1)
	c.Check(len(m.queryColumns), tc.Equals, 1)
	c.Check(m.queryCount, tc.Equals, 1)
}

func (s *dbDebugSuite) TestHardcodedAllMsgPreselect(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.preSelectDatabase = "lxd-pilot"
	msg := hardcodedAllMsg{
		databases: []DqliteDatabase{
			{Name: "controller", Namespace: "controller", Type: "controller"},
			{Name: "lxd-pilot", Namespace: "ns2", Type: "model"},
		},
	}
	m, _ = step(m, msg)
	c.Check(m.selectedDB, tc.Equals, 1)
}

// ---------------------------------------------------------------------------
// load*Msg handler tests
// ---------------------------------------------------------------------------

func (s *dbDebugSuite) TestLoadDatabasesMsgPopulates(c *tc.C) {
	mock := &recordedMockAPI{
		objects:      []DqliteObject{{Name: "foo", Kind: "table"}},
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1", Role: "voter"}},
	}
	m := NewDqliteModel(mock)
	m.width = 100
	m.height = 40

	dbs := []DqliteDatabase{
		{Name: "controller", Namespace: "controller", Type: "controller"},
	}
	m, cmd := step(m, loadDatabasesMsg{databases: dbs})
	c.Check(len(m.databases), tc.Equals, 1)
	c.Check(m.databases[0].Name, tc.Equals, "controller")
	c.Check(m.selectedDB, tc.Equals, 0)
	c.Check(cmd, tc.NotNil)
}

func (s *dbDebugSuite) TestLoadDatabasesMsgPreselectMatches(c *tc.C) {
	mock := &recordedMockAPI{
		objects:      []DqliteObject{{Name: "foo", Kind: "table"}},
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1", Role: "voter"}},
	}
	m := NewDqliteModel(mock)
	m.preSelectDatabase = "lxd-pilot"
	m.width = 100
	m.height = 40

	dbs := []DqliteDatabase{
		{Name: "controller", Namespace: "ctrl", Type: "controller"},
		{Name: "lxd-pilot", Namespace: "ns2", Type: "model"},
	}
	m, cmd := step(m, loadDatabasesMsg{databases: dbs})
	c.Check(m.selectedDB, tc.Equals, 1)
	c.Check(cmd, tc.NotNil)
}

func (s *dbDebugSuite) TestLoadDatabasesMsgPreselectNoMatch(c *tc.C) {
	mock := &recordedMockAPI{
		objects:      []DqliteObject{{Name: "foo", Kind: "table"}},
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1", Role: "voter"}},
	}
	m := NewDqliteModel(mock)
	m.preSelectDatabase = "nonexistent"
	m.width = 100
	m.height = 40

	dbs := []DqliteDatabase{
		{Name: "controller", Namespace: "ctrl", Type: "controller"},
	}
	m, cmd := step(m, loadDatabasesMsg{databases: dbs})
	c.Check(m.selectedDB, tc.Equals, 0)
	c.Check(cmd, tc.NotNil)
}

func (s *dbDebugSuite) TestLoadDatabasesMsgError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, cmd := step(m, loadDatabasesMsg{err: errors.New("connection failed")})
	c.Check(m.err, tc.Equals, "connection failed")
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadDatabasesMsgEmpty(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 100
	m.height = 40

	m, cmd := step(m, loadDatabasesMsg{})
	c.Check(len(m.databases), tc.Equals, 0)
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadObjectsMsgPopulates(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	objs := []DqliteObject{
		{Name: "foo", Kind: "table"},
		{Name: "bar", Kind: "view"},
	}
	m, _ = step(m, loadObjectsMsg{objects: objs})
	c.Check(len(m.objects), tc.Equals, 2)
	c.Check(m.objects[0].Name, tc.Equals, "foo")
	c.Check(m.selectedObj, tc.Equals, 0)
}

func (s *dbDebugSuite) TestLoadObjectsMsgError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, cmd := step(m, loadObjectsMsg{err: errors.New("objects failed")})
	c.Check(m.err, tc.Equals, "objects failed")
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadDDLMsgPopulates(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, _ = step(m, loadDDLMsg{ddl: "CREATE TABLE t (id INTEGER)"})
	c.Check(m.ddl, tc.Equals, "CREATE TABLE t (id INTEGER)")
}

func (s *dbDebugSuite) TestLoadDDLMsgError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, cmd := step(m, loadDDLMsg{err: errors.New("ddl failed")})
	c.Check(m.err, tc.Equals, "ddl failed")
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadQueryMsgPopulates(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.queryError = "previous error"
	result := &DqliteQueryResult{
		Columns:   []string{"c1", "c2"},
		Rows:      [][]string{{"a", "b"}, {"c", "d"}},
		Count:     2,
		Truncated: true,
	}
	m, _ = step(m, loadQueryMsg{result: result})
	c.Check(len(m.queryColumns), tc.Equals, 2)
	c.Check(len(m.queryRows), tc.Equals, 2)
	c.Check(m.queryCount, tc.Equals, 2)
	c.Check(m.queryTruncated, tc.IsTrue)
	c.Check(m.queryError, tc.Equals, "")
}

func (s *dbDebugSuite) TestLoadQueryMsgError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, cmd := step(m, loadQueryMsg{err: errors.New("query failed")})
	c.Check(m.queryError, tc.Equals, "query failed")
	c.Check(m.err, tc.Equals, "")
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadClusterMsgPopulates(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	nodes := []DqliteNode{
		{ID: "aa", Address: "10.0.0.1:12345", Role: "voter"},
	}
	m, _ = step(m, loadClusterMsg{nodes: nodes})
	c.Check(len(m.clusterNodes), tc.Equals, 1)
	c.Check(m.clusterNodes[0].ID, tc.Equals, "aa")
}

func (s *dbDebugSuite) TestLoadClusterMsgError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, cmd := step(m, loadClusterMsg{err: errors.New("cluster failed")})
	c.Check(m.err, tc.Equals, "cluster failed")
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestErrMsg(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m, _ = step(m, errMsg{err: errors.New("something went wrong")})
	c.Check(m.err, tc.Equals, "something went wrong")
}

// ---------------------------------------------------------------------------
// Key binding tests
// ---------------------------------------------------------------------------

func (s *dbDebugSuite) TestTabCyclesFocus(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50

	c.Check(m.focus, tc.Equals, dqlitePaneDatabases)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyTab})
	c.Check(m.focus, tc.Equals, dqlitePaneObjects)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyTab})
	c.Check(m.focus, tc.Equals, dqlitePaneQuery)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyTab})
	c.Check(m.focus, tc.Equals, dqlitePaneCluster)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyTab})
	c.Check(m.focus, tc.Equals, dqlitePaneDatabases)
}

func (s *dbDebugSuite) TestShiftTabCyclesFocus(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50

	c.Check(m.focus, tc.Equals, dqlitePaneDatabases)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	c.Check(m.focus, tc.Equals, dqlitePaneCluster)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	c.Check(m.focus, tc.Equals, dqlitePaneQuery)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	c.Check(m.focus, tc.Equals, dqlitePaneObjects)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	c.Check(m.focus, tc.Equals, dqlitePaneDatabases)
}

func (s *dbDebugSuite) TestUpDownDatabases(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{
		{Name: "db1", Namespace: "ns1"},
		{Name: "db2", Namespace: "ns2"},
		{Name: "db3", Namespace: "ns3"},
	}
	m.focus = dqlitePaneDatabases

	c.Check(m.selectedDB, tc.Equals, 0)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyDown})
	c.Check(m.selectedDB, tc.Equals, 1)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyDown})
	c.Check(m.selectedDB, tc.Equals, 2)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyDown})
	c.Check(m.selectedDB, tc.Equals, 2)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyUp})
	c.Check(m.selectedDB, tc.Equals, 1)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyUp})
	c.Check(m.selectedDB, tc.Equals, 0)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyUp})
	c.Check(m.selectedDB, tc.Equals, 0)
}

func (s *dbDebugSuite) TestUpDownObjects(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.objects = []DqliteObject{
		{Name: "obj1", Kind: "table"},
		{Name: "obj2", Kind: "table"},
	}
	m.focus = dqlitePaneObjects

	c.Check(m.selectedObj, tc.Equals, 0)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyDown})
	c.Check(m.selectedObj, tc.Equals, 1)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyDown})
	c.Check(m.selectedObj, tc.Equals, 1)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyUp})
	c.Check(m.selectedObj, tc.Equals, 0)
}

func (s *dbDebugSuite) TestEnterDatabasesFiresLoadObjects(c *tc.C) {
	mock := &recordedMockAPI{
		objects: []DqliteObject{{Name: "foo", Kind: "table"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneDatabases

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyEnter})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadObjectsMsg)
	c.Check(ok, tc.IsTrue)
	c.Check(len(mock.objectsCalls), tc.Equals, 1)
	c.Check(mock.objectsCalls[0].ns, tc.Equals, "ns1")
}

func (s *dbDebugSuite) TestEnterDatabasesEmpty(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.focus = dqlitePaneDatabases

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyEnter})
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestEnterObjectsFiresLoadDDL(c *tc.C) {
	mock := &recordedMockAPI{ddl: "CREATE TABLE t (id INTEGER)"}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.objects = []DqliteObject{{Name: "foo", Kind: "table"}}
	m.focus = dqlitePaneObjects

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyEnter})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadDDLMsg)
	c.Check(ok, tc.IsTrue)
	c.Check(len(mock.ddlCalls), tc.Equals, 1)
	c.Check(mock.ddlCalls[0].ns, tc.Equals, "ns1")
	c.Check(mock.ddlCalls[0].name, tc.Equals, "foo")
}

func (s *dbDebugSuite) TestEnterObjectsEmpty(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.focus = dqlitePaneObjects

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyEnter})
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestCtrlHTogglesHelp(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50

	c.Check(m.showHelp, tc.IsFalse)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyCtrlH})
	c.Check(m.showHelp, tc.IsTrue)

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyCtrlH})
	c.Check(m.showHelp, tc.IsFalse)
}

func (s *dbDebugSuite) TestCtrlCQuits(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50

	c.Check(m.quitting, tc.IsFalse)

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	c.Check(m.quitting, tc.IsTrue)
	c.Check(cmd, tc.NotNil)
}

func (s *dbDebugSuite) TestEscDismissesHelp(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.showHelp = true

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyEsc})
	c.Check(m.showHelp, tc.IsFalse)
}

func (s *dbDebugSuite) TestEscBlursQuery(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.focus = dqlitePaneQuery

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyEsc})
	c.Check(m.queryInput.Focused(), tc.IsFalse)
	c.Check(m.focus, tc.Equals, dqlitePaneQuery)
}

func (s *dbDebugSuite) TestEscClearsError(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.err = "some error"

	m, _ = step(m, tea.KeyMsg{Type: tea.KeyEsc})
	c.Check(m.err, tc.Equals, "")
}

func (s *dbDebugSuite) TestAlphanumericKeysInQuery(c *tc.C) {
	mock := &recordedMockAPI{}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.focus = dqlitePaneQuery
	m.queryInput.Focus()

	for _, r := range []rune{'q', 's', 'r', 'p'} {
		m, _ = step(m, tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{r},
		})
	}
c.Check(m.queryInput.Value(), tc.Not(tc.Equals), "")
}

func (s *dbDebugSuite) TestCtrlRRefreshDatabases(c *tc.C) {
	mock := &recordedMockAPI{
		databases: []DqliteDatabase{{Name: "refreshed", Namespace: "ns"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneDatabases

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadDatabasesMsg)
	c.Check(ok, tc.IsTrue)
}

func (s *dbDebugSuite) TestCtrlRRefreshObjects(c *tc.C) {
	mock := &recordedMockAPI{
		objects: []DqliteObject{{Name: "foo", Kind: "table"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneObjects

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadObjectsMsg)
	c.Check(ok, tc.IsTrue)
}

func (s *dbDebugSuite) TestCtrlRRefreshCluster(c *tc.C) {
	mock := &recordedMockAPI{
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1", Role: "voter"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneCluster

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadClusterMsg)
	c.Check(ok, tc.IsTrue)
}

func (s *dbDebugSuite) TestCtrlRRefreshQueryWithSQL(c *tc.C) {
	mock := &recordedMockAPI{
		queryResult: &DqliteQueryResult{Columns: []string{"c1"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneQuery
	m.queryInput.SetValue("SELECT 1")

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadQueryMsg)
	c.Check(ok, tc.IsTrue)
}

func (s *dbDebugSuite) TestCtrlRRefreshQueryEmptySQL(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.focus = dqlitePaneQuery

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestCtrlRRefreshEmptyDatabases(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.focus = dqlitePaneDatabases

	_, cmd := step(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestReloadObjectsUsesKind(c *tc.C) {
	mock := &recordedMockAPI{
		objects: []DqliteObject{{Name: "foo", Kind: "table"}},
	}
	m := NewDqliteModel(mock)
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "mydb", Namespace: "ns1"}}
	m.kind = "view"

	cmd := m.reloadObjects()
	c.Assert(cmd, tc.NotNil)
	msg := execCmd(cmd)
	_, ok := msg.(loadObjectsMsg)
	c.Check(ok, tc.IsTrue)
	c.Check(len(mock.objectsCalls), tc.Equals, 1)
	c.Check(mock.objectsCalls[0].kind, tc.Equals, "view")
}

func (s *dbDebugSuite) TestReloadObjectsEmpty(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	cmd := m.reloadObjects()
	c.Check(cmd, tc.IsNil)
}

func (s *dbDebugSuite) TestLoadDatabasesCmdCallsAPI(c *tc.C) {
	mock := &recordedMockAPI{
		databases: []DqliteDatabase{{Name: "db", Namespace: "ns"}},
	}
	cmd := loadDatabasesCmd(mock)
	msg := execCmd(cmd)
	got, ok := msg.(loadDatabasesMsg)
	c.Assert(ok, tc.IsTrue)
	c.Check(len(got.databases), tc.Equals, 1)
	c.Check(mock.databasesCalls, tc.Equals, 1)
}

func (s *dbDebugSuite) TestLoadObjectsCmdCallsAPI(c *tc.C) {
	mock := &recordedMockAPI{
		objects: []DqliteObject{{Name: "foo", Kind: "table"}},
	}
	cmd := loadObjectsCmd(mock, "ns1", "table")
	msg := execCmd(cmd)
	got, ok := msg.(loadObjectsMsg)
	c.Assert(ok, tc.IsTrue)
	c.Check(len(got.objects), tc.Equals, 1)
	c.Check(len(mock.objectsCalls), tc.Equals, 1)
	c.Check(mock.objectsCalls[0].ns, tc.Equals, "ns1")
	c.Check(mock.objectsCalls[0].kind, tc.Equals, "table")
}

func (s *dbDebugSuite) TestLoadDDLCmdCallsAPI(c *tc.C) {
	mock := &recordedMockAPI{ddl: "CREATE TABLE t (id INT)"}
	cmd := loadDDLCmd(mock, "ns1", "foo")
	msg := execCmd(cmd)
	got, ok := msg.(loadDDLMsg)
	c.Assert(ok, tc.IsTrue)
	c.Check(got.ddl, tc.Equals, "CREATE TABLE t (id INT)")
	c.Check(len(mock.ddlCalls), tc.Equals, 1)
	c.Check(mock.ddlCalls[0].ns, tc.Equals, "ns1")
	c.Check(mock.ddlCalls[0].name, tc.Equals, "foo")
}

func (s *dbDebugSuite) TestLoadQueryCmdCallsAPI(c *tc.C) {
	mock := &recordedMockAPI{
		queryResult: &DqliteQueryResult{Columns: []string{"c1"}, Count: 1},
	}
	cmd := loadQueryCmd(mock, "ns1", "SELECT 1", 50)
	msg := execCmd(cmd)
	got, ok := msg.(loadQueryMsg)
	c.Assert(ok, tc.IsTrue)
	c.Check(len(got.result.Columns), tc.Equals, 1)
	c.Check(len(mock.queryCalls), tc.Equals, 1)
	c.Check(mock.queryCalls[0].ns, tc.Equals, "ns1")
	c.Check(mock.queryCalls[0].sql, tc.Equals, "SELECT 1")
	c.Check(mock.queryCalls[0].limit, tc.Equals, 50)
}

func (s *dbDebugSuite) TestLoadClusterCmdCallsAPI(c *tc.C) {
	mock := &recordedMockAPI{
		clusterNodes: []DqliteNode{{ID: "aa", Address: "10.0.0.1", Role: "voter"}},
	}
	cmd := loadClusterCmd(mock)
	msg := execCmd(cmd)
	got, ok := msg.(loadClusterMsg)
	c.Assert(ok, tc.IsTrue)
	c.Check(len(got.nodes), tc.Equals, 1)
	c.Check(mock.clusterCalls, tc.Equals, 1)
}

// ---------------------------------------------------------------------------
// View tests
// ---------------------------------------------------------------------------

func (s *dbDebugSuite) TestViewQuitting(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.quitting = true

	c.Check(m.View(), tc.Equals, "")
}

func (s *dbDebugSuite) TestViewHelp(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.showHelp = true

	v := m.View()
	c.Check(v, tc.Not(tc.Equals), "")
}

func (s *dbDebugSuite) TestViewLoading(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	c.Check(m.View(), tc.Equals, "Loading...")
}

func (s *dbDebugSuite) TestViewWithData(c *tc.C) {
	m := NewDqliteModel(&recordedMockAPI{})
	m.width = 120
	m.height = 50
	m.databases = []DqliteDatabase{{Name: "controller", Namespace: "controller", Type: "controller"}}
	m.clusterNodes = []DqliteNode{{ID: "aa", Address: "10.0.0.1:12345", Role: "voter"}}
	m.objects = []DqliteObject{{Name: "foo", Kind: "table"}}
	m.ddl = "CREATE TABLE foo (id INTEGER)"
	m.queryColumns = []string{"id"}
	m.queryRows = [][]string{{"1"}}
	m.queryCount = 1

	v := m.View()
	c.Check(v, tc.Not(tc.Equals), "")
	c.Check(v, tc.Not(tc.Equals), "Loading...")
}
