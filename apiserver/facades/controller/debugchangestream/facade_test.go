// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debugchangestream_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/juju/names/v6"
	"github.com/juju/tc"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/apiserver/facades/controller/debugchangestream"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/core/model"
	debugchangestreamservice "github.com/juju/juju/domain/debugchangestream/service"
	"github.com/juju/juju/rpc/params"
)

const (
	controllerUUID = "deadbeef-dead-dead-dead-deaddeadbeef"
	model1UUID     = "cafecafe-cafe-cafe-cafe-cafecafecafe"
	model2UUID     = "beefdead-beef-beef-beef-beefdeadbeef"
)

// stubChangeStreamSvc is a manual stub for DebugChangeStreamService.
type stubChangeStreamSvc struct {
	pauseErr  error
	stepRes   []debugchangestreamservice.StepResult
	stepErr   error
	resumeErr error
}

func (s *stubChangeStreamSvc) Pause(_ context.Context) error {
	return s.pauseErr
}

func (s *stubChangeStreamSvc) Step(
	_ context.Context, _ int,
) ([]debugchangestreamservice.StepResult, error) {
	return s.stepRes, s.stepErr
}

func (s *stubChangeStreamSvc) Resume(_ context.Context) error {
	return s.resumeErr
}

// stubModelListSvc is a manual stub for ModelListService.
type stubModelListSvc struct {
	models []model.Model
	err    error
}

func (s *stubModelListSvc) GetAllModels(
	_ context.Context,
) ([]model.Model, error) {
	return s.models, s.err
}

// Suite holds the state for facade unit tests.
type Suite struct {
	controllerSvc *stubChangeStreamSvc
	modelListSvc  *stubModelListSvc
	controllerTag names.ControllerTag
}

// TestSuite runs the facade test suite.
func TestSuite(t *testing.T) {
	tc.Run(t, &Suite{})
}

func (s *Suite) SetUpTest(c *tc.C) {
	s.controllerSvc = &stubChangeStreamSvc{}
	s.modelListSvc = &stubModelListSvc{}
	s.controllerTag = names.NewControllerTag(controllerUUID)
}

func (s *Suite) newAPI(
	auth apiservertesting.FakeAuthorizer,
	modelSvcGetter debugchangestream.ModelServiceGetter,
) *debugchangestream.API {
	return debugchangestream.NewAPI(
		auth,
		s.controllerTag,
		s.controllerSvc,
		s.modelListSvc,
		modelSvcGetter,
	)
}

// TestPauseNonClientAgent verifies that a non-client agent is
// rejected with ErrPerm.
func (s *Suite) TestPauseNonClientAgent(c *tc.C) {
	auth := apiservertesting.FakeAuthorizer{
		Tag: names.NewMachineTag("0"),
	}
	api := s.newAPI(auth, nil)

	_, err := api.Pause(c.Context(), params.DebugChangeStreamArgs{
		Target: params.DebugChangeStreamTarget{Controller: true},
	})
	c.Assert(err, tc.ErrorIs, apiservererrors.ErrPerm)
}

// TestPauseInsufficientPermission verifies that a client without
// superuser access receives a permission error.
func (s *Suite) TestPauseInsufficientPermission(c *tc.C) {
	auth := apiservertesting.FakeAuthorizer{
		Tag: names.NewUserTag("normaluser"),
	}
	api := s.newAPI(auth, nil)

	_, err := api.Pause(c.Context(), params.DebugChangeStreamArgs{
		Target: params.DebugChangeStreamTarget{Controller: true},
	})
	c.Assert(err, tc.ErrorIs, apiservererrors.ErrPerm)
}

// TestStepAllTargetAggregatesResults verifies that Step with
// Target.All=true calls the controller and all model services and
// collects results for all three databases.
func (s *Suite) TestStepAllTargetAggregatesResults(c *tc.C) {
	auth := apiservertesting.FakeAuthorizer{
		Tag: names.NewUserTag("superuser"),
	}
	model1Svc := &stubChangeStreamSvc{
		stepRes: []debugchangestreamservice.StepResult{
			{TxnMin: 1, TxnMax: 5, EventCount: 4},
		},
	}
	model2Svc := &stubChangeStreamSvc{
		stepRes: []debugchangestreamservice.StepResult{
			{TxnMin: 2, TxnMax: 6, EventCount: 3},
		},
	}
	s.modelListSvc.models = []model.Model{
		{UUID: model.UUID(model1UUID)},
		{UUID: model.UUID(model2UUID)},
	}
	modelSvcGetter := func(
		_ context.Context, uuid model.UUID,
	) (debugchangestream.DebugChangeStreamService, error) {
		switch uuid.String() {
		case model1UUID:
			return model1Svc, nil
		case model2UUID:
			return model2Svc, nil
		}
		return nil, fmt.Errorf("unexpected model UUID %s", uuid)
	}

	api := s.newAPI(auth, modelSvcGetter)

	result, err := api.Step(c.Context(), params.DebugChangeStreamStepArgs{
		Target: params.DebugChangeStreamTarget{All: true},
		Count:  1,
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(len(result.Results), tc.Equals, 3)
}
