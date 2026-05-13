// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

type loadDatabasesMsg struct {
	databases []DqliteDatabase
	err       error
}

type loadObjectsMsg struct {
	objects []DqliteObject
	err     error
}

type loadDDLMsg struct {
	ddl string
	err error
}

type loadQueryMsg struct {
	result *DqliteQueryResult
	err    error
}

type loadClusterMsg struct {
	nodes []DqliteNode
	err   error
}

type errMsg struct{ err error }

func loadDatabasesCmd(api DqliteAPI) tea.Cmd {
	return func() tea.Msg {
		dbs, err := api.Databases(context.Background())
		return loadDatabasesMsg{databases: dbs, err: err}
	}
}

func loadObjectsCmd(api DqliteAPI, ns, kind string) tea.Cmd {
	return func() tea.Msg {
		objs, err := api.Objects(context.Background(), ns, kind)
		return loadObjectsMsg{objects: objs, err: err}
	}
}

func loadDDLCmd(api DqliteAPI, ns, name string) tea.Cmd {
	return func() tea.Msg {
		ddl, err := api.DDL(context.Background(), ns, name)
		return loadDDLMsg{ddl: ddl, err: err}
	}
}

func loadQueryCmd(api DqliteAPI, ns, sql string, limit int) tea.Cmd {
	return func() tea.Msg {
		result, err := api.Query(context.Background(), ns, sql, limit)
		return loadQueryMsg{result: result, err: err}
	}
}

func loadClusterCmd(api DqliteAPI) tea.Cmd {
	return func() tea.Msg {
		nodes, err := api.Cluster(context.Background())
		return loadClusterMsg{nodes: nodes, err: err}
	}
}
