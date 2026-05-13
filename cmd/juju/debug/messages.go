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
