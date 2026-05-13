// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
)

type mockDebugChangeStreamAPI struct{}

func newMockDebugChangeStreamAPI() *mockDebugChangeStreamAPI {
	return &mockDebugChangeStreamAPI{}
}

func (m *mockDebugChangeStreamAPI) Status(_ context.Context) ([]StreamStatus, error) {
	return []StreamStatus{
		{Name: "controller", State: "RUNNING", TxnID: 200},
		{Name: "mymodel", State: "RUNNING", TxnID: 150},
		{Name: "othermodel", State: "PAUSED", TxnID: 80},
	}, nil
}

func (m *mockDebugChangeStreamAPI) Pause(_ context.Context, _ string) error {
	return nil
}

func (m *mockDebugChangeStreamAPI) Step(_ context.Context, _ string, _ int) ([]StepResult, error) {
	return nil, nil
}

func (m *mockDebugChangeStreamAPI) Resume(_ context.Context, _ string) error {
	return nil
}

func (m *mockDebugChangeStreamAPI) Close() error {
	return nil
}

type mockModelListAPI struct{}

func newMockModelListAPI() *mockModelListAPI {
	return &mockModelListAPI{}
}

func (m *mockModelListAPI) ListModels(_ context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{Name: "controller", UUID: "ctrl-uuid", IsController: true},
		{Name: "mymodel", UUID: "model-uuid-1", IsController: false},
		{Name: "othermodel", UUID: "model-uuid-2", IsController: false},
	}, nil
}

func (m *mockModelListAPI) Close() error {
	return nil
}
