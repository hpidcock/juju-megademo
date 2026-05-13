// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"time"
)

type selectTxnMsg struct {
	txnIndex int
}

type changestreamTickMsg time.Time
