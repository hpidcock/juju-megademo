// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debugchangestream

import (
	"context"
	"reflect"

	"github.com/juju/juju/apiserver/facade"
)

// Register is called to expose a package of facades onto a given registry.
func Register(registry facade.FacadeRegistry) {
	registry.MustRegisterForMultiModel(
		"DebugChangeStream", 1,
		func(
			stdCtx context.Context,
			ctx facade.MultiModelContext,
		) (facade.Facade, error) {
			return newFacade(stdCtx, ctx)
		},
		reflect.TypeFor[*API](),
	)
}
