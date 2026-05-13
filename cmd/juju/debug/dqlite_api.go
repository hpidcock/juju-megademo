// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
)

type DqliteDatabase struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	UUID      string `json:"uuid,omitempty"`
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
	Rows      [][]string  `json:"rows"`
	Count     int         `json:"count"`
	Truncated bool        `json:"truncated"`
}

type DqliteAPI interface {
	Databases(ctx context.Context) ([]DqliteDatabase, error)
	Objects(ctx context.Context, ns, kind string) ([]DqliteObject, error)
	DDL(ctx context.Context, ns, name string) (string, error)
	Query(ctx context.Context, ns, sql string, limit int) (*DqliteQueryResult, error)
	Cluster(ctx context.Context) ([]DqliteNode, error)
}

type dqliteAPIImpl struct{}

func NewDqliteAPI() DqliteAPI {
	return &dqliteAPIImpl{}
}

func (a *dqliteAPIImpl) Databases(_ context.Context) ([]DqliteDatabase, error) {
	panic("not implemented")
}

func (a *dqliteAPIImpl) Objects(_ context.Context, _, _ string) ([]DqliteObject, error) {
	panic("not implemented")
}

func (a *dqliteAPIImpl) DDL(_ context.Context, _, _ string) (string, error) {
	panic("not implemented")
}

func (a *dqliteAPIImpl) Query(_ context.Context, _ string, _ string, _ int) (*DqliteQueryResult, error) {
	panic("not implemented")
}

func (a *dqliteAPIImpl) Cluster(_ context.Context) ([]DqliteNode, error) {
	panic("not implemented")
}
