// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"

	"github.com/juju/juju/api/common"
)

type DqliteAPI interface {
	Databases(ctx context.Context) ([]common.DqliteDatabase, error)
	Objects(ctx context.Context, ns, kind string) ([]common.DqliteObject, error)
	DDL(ctx context.Context, ns, name string) (string, error)
	Query(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error)
	Cluster(ctx context.Context) ([]common.DqliteNode, error)
}

type dqliteAPIImpl struct {
	client *common.DqliteClient
}

func NewDqliteAPI(client *common.DqliteClient) DqliteAPI {
	return &dqliteAPIImpl{client: client}
}

func (a *dqliteAPIImpl) Databases(ctx context.Context) ([]common.DqliteDatabase, error) {
	return a.client.Databases(ctx)
}

func (a *dqliteAPIImpl) Objects(ctx context.Context, ns, kind string) ([]common.DqliteObject, error) {
	return a.client.Objects(ctx, ns, kind)
}

func (a *dqliteAPIImpl) DDL(ctx context.Context, ns, name string) (string, error) {
	return a.client.DDL(ctx, ns, name)
}

func (a *dqliteAPIImpl) Query(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error) {
	return a.client.Query(ctx, ns, sql, limit)
}

func (a *dqliteAPIImpl) Cluster(ctx context.Context) ([]common.DqliteNode, error) {
	return a.client.Cluster(ctx)
}
