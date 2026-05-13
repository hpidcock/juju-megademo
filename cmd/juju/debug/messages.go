// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"time"

	"github.com/juju/juju/api/common"
)

type selectTxnMsg struct {
	txnIndex int
}

type switchModelMsg struct {
	modelUUID string
	modelName string
}

type listModelsMsg struct {
	models []ModelInfo
	err    error
	open   bool
}

type statusTickMsg time.Time

type changestreamTickMsg time.Time

type logMsg struct {
	record  common.LogMessage
	version int
}

type logStreamReadyMsg struct {
	ch      <-chan common.LogMessage
	cancel  context.CancelFunc
	version int
	err     error
}

type logStreamDoneMsg struct {
	version int
}

type fetchTraceResultMsg struct {
	traceID string
	data    *TraceData
	err     error
}
