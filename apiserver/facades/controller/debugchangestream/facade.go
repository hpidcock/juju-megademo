// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debugchangestream

import (
	"context"
	"sync"

	"github.com/juju/names/v6"
	"golang.org/x/sync/errgroup"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/permission"
	debugchangestreamservice "github.com/juju/juju/domain/debugchangestream/service"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/services"
	"github.com/juju/juju/rpc/params"
)

// DebugChangeStreamService is the subset of
// domain/debugchangestream/service.Service used by this facade.
type DebugChangeStreamService interface {
	Pause(ctx context.Context) error
	Step(
		ctx context.Context, count int,
	) ([]debugchangestreamservice.StepResult, error)
	Resume(ctx context.Context) error
	Status(ctx context.Context) (string, error)
	CurrentTxnID(ctx context.Context) (int64, error)
}

// ModelListService lists all models known to the controller.
type ModelListService interface {
	GetAllModels(ctx context.Context) ([]model.Model, error)
}

// ModelServiceGetter returns a DebugChangeStreamService for a given
// model UUID.
type ModelServiceGetter func(
	ctx context.Context, modelUUID model.UUID,
) (DebugChangeStreamService, error)

// API implements the DebugChangeStream facade.
type API struct {
	auth           facade.Authorizer
	controllerTag  names.ControllerTag
	controllerSvc  DebugChangeStreamService
	modelListSvc   ModelListService
	modelSvcGetter ModelServiceGetter
}

// NewAPI constructs the facade from explicit dependencies. This is used
// directly in unit tests.
func NewAPI(
	auth facade.Authorizer,
	controllerTag names.ControllerTag,
	controllerSvc DebugChangeStreamService,
	modelListSvc ModelListService,
	modelSvcGetter ModelServiceGetter,
) *API {
	return &API{
		auth:           auth,
		controllerTag:  controllerTag,
		controllerSvc:  controllerSvc,
		modelListSvc:   modelListSvc,
		modelSvcGetter: modelSvcGetter,
	}
}

// newFacade constructs an API from a MultiModelContext.
func newFacade(
	_ context.Context,
	ctx facade.MultiModelContext,
) (*API, error) {
	domSvc := ctx.DomainServices()
	cdsp, ok := domSvc.(services.ControllerDomainServicesProvider)
	if !ok {
		return nil, errors.New(
			"domain services do not provide controller services",
		)
	}
	controllerSvc := cdsp.ControllerDomainSvc().DebugChangeStream()
	modelListSvc := cdsp.ControllerDomainSvc().Model()

	modelSvcGetter := func(
		mctx context.Context, uuid model.UUID,
	) (DebugChangeStreamService, error) {
		svc, err := ctx.DomainServicesForModel(mctx, uuid)
		if err != nil {
			return nil, errors.Errorf(
				"getting domain services for model %s: %w",
				uuid, err,
			)
		}
		return svc.DebugChangeStream(), nil
	}

	controllerUUID := ctx.ControllerUUID()
	controllerTag := names.NewControllerTag(controllerUUID)

	return NewAPI(
		ctx.Auth(),
		controllerTag,
		controllerSvc,
		modelListSvc,
		modelSvcGetter,
	), nil
}

// checkAuth verifies the caller is a superuser client.
func (api *API) checkAuth(ctx context.Context) error {
	if !api.auth.AuthClient() {
		return apiservererrors.ErrPerm
	}
	return api.auth.HasPermission(
		ctx, permission.SuperuserAccess, api.controllerTag,
	)
}

// targetServices resolves a DebugChangeStreamTarget to a list of
// (service, name) pairs. Names are "controller" or model UUID strings.
func (api *API) targetServices(
	ctx context.Context,
	target params.DebugChangeStreamTarget,
) ([]DebugChangeStreamService, []string, error) {
	switch {
	case target.All:
		svcs := []DebugChangeStreamService{api.controllerSvc}
		names := []string{"controller"}
		models, err := api.modelListSvc.GetAllModels(ctx)
		if err != nil {
			return nil, nil, errors.Errorf(
				"listing models: %w", err,
			)
		}
		for _, m := range models {
			svc, err := api.modelSvcGetter(ctx, m.UUID)
			if err != nil {
				return nil, nil, errors.Errorf(
					"getting service for model %s: %w",
					m.UUID, err,
				)
			}
			svcs = append(svcs, svc)
			names = append(names, m.UUID.String())
		}
		return svcs, names, nil
	case target.Controller:
		return []DebugChangeStreamService{api.controllerSvc},
			[]string{"controller"}, nil
	case target.ModelUUID != "":
		uuid := model.UUID(target.ModelUUID)
		svc, err := api.modelSvcGetter(ctx, uuid)
		if err != nil {
			return nil, nil, errors.Errorf(
				"getting service for model %s: %w", uuid, err,
			)
		}
		return []DebugChangeStreamService{svc},
			[]string{target.ModelUUID}, nil
	default:
		return nil, nil, errors.New(
			"no target specified: set model-uuid, controller, or all",
		)
	}
}

// Pause pauses the targeted changestream(s).
func (api *API) Pause(
	ctx context.Context, args params.DebugChangeStreamArgs,
) (params.DebugChangeStreamPauseResult, error) {
	if err := api.checkAuth(ctx); err != nil {
		return params.DebugChangeStreamPauseResult{}, err
	}
	svcs, names, err := api.targetServices(ctx, args.Target)
	if err != nil {
		return params.DebugChangeStreamPauseResult{}, err
	}

	results := make([]params.DebugChangeStreamDBResult, len(svcs))
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	for i, svc := range svcs {
		i, svc, name := i, svc, names[i]
		g.Go(func() error {
			pauseErr := svc.Pause(gCtx)
			mu.Lock()
			results[i] = params.DebugChangeStreamDBResult{
				Name:  name,
				Error: apiservererrors.ServerError(pauseErr),
			}
			mu.Unlock()
			return nil
		})
	}
	// Errors are captured per-result; g.Wait() always returns nil.
	_ = g.Wait()
	return params.DebugChangeStreamPauseResult{Results: results}, nil
}

// Step advances the paused changestream(s) by args.Count transactions.
func (api *API) Step(
	ctx context.Context, args params.DebugChangeStreamStepArgs,
) (params.DebugChangeStreamStepResult, error) {
	if err := api.checkAuth(ctx); err != nil {
		return params.DebugChangeStreamStepResult{}, err
	}
	svcs, names, err := api.targetServices(ctx, args.Target)
	if err != nil {
		return params.DebugChangeStreamStepResult{}, err
	}

	results := make([]params.DebugChangeStreamDBResult, len(svcs))
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	for i, svc := range svcs {
		i, svc, name := i, svc, names[i]
		g.Go(func() error {
			stepResults, stepErr := svc.Step(gCtx, args.Count)
			res := params.DebugChangeStreamDBResult{
				Name:  name,
				Error: apiservererrors.ServerError(stepErr),
			}
			if stepErr == nil && len(stepResults) > 0 {
				res.TxnMin = stepResults[0].TxnMin
				res.TxnMax = stepResults[len(stepResults)-1].TxnMax
				last := stepResults[len(stepResults)-1]
				res.TraceID = last.TraceID
				res.SpanID = last.SpanID
				for _, r := range stepResults {
					res.EventCount += r.EventCount
				}
			}
			mu.Lock()
			results[i] = res
			mu.Unlock()
			return nil
		})
	}
	// Errors are captured per-result; g.Wait() always returns nil.
	_ = g.Wait()
	return params.DebugChangeStreamStepResult{Results: results}, nil
}

// Resume resumes the targeted changestream(s).
func (api *API) Resume(
	ctx context.Context, args params.DebugChangeStreamArgs,
) error {
	if err := api.checkAuth(ctx); err != nil {
		return err
	}
	svcs, _, err := api.targetServices(ctx, args.Target)
	if err != nil {
		return err
	}

	g, gCtx := errgroup.WithContext(ctx)
	for _, svc := range svcs {
		svc := svc
		g.Go(func() error {
			return svc.Resume(gCtx)
		})
	}
	return g.Wait()
}

// Status returns the debug state and current txn_id for all databases.
func (api *API) Status(
	ctx context.Context,
) (params.DebugChangeStreamStatusResult, error) {
	if err := api.checkAuth(ctx); err != nil {
		return params.DebugChangeStreamStatusResult{}, err
	}

	type dbInfo struct {
		svc  DebugChangeStreamService
		name string
	}

	dbs := []dbInfo{{svc: api.controllerSvc, name: "controller"}}

	models, err := api.modelListSvc.GetAllModels(ctx)
	if err != nil {
		return params.DebugChangeStreamStatusResult{}, errors.Errorf(
			"listing models: %w", err,
		)
	}
	for _, m := range models {
		svc, svcErr := api.modelSvcGetter(ctx, m.UUID)
		if svcErr != nil {
			continue
		}
		dbs = append(dbs, dbInfo{svc: svc, name: m.UUID.String()})
	}

	results := make([]params.DebugChangeStreamDBStatus, len(dbs))
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	for i, db := range dbs {
		i, db := i, db
		g.Go(func() error {
			state, stateErr := db.svc.Status(gCtx)
			txnID, txnErr := db.svc.CurrentTxnID(gCtx)
			mu.Lock()
			results[i] = params.DebugChangeStreamDBStatus{
				Name:  db.name,
				State: state,
				TxnID: txnID,
				Error: apiservererrors.ServerError(
					errors.Join(stateErr, txnErr),
				),
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return params.DebugChangeStreamStatusResult{Results: results}, nil
}
