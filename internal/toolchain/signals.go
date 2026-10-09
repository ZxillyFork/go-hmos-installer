// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package toolchain

import (
	"os"
	"os/signal"
)

// A terminal sends these signals to both wrapper and child. Suppress the
// wrapper's default runtime handlers (especially SIGQUIT's stack dump) and let
// the Go child handle them. Notify rather than Ignore preserves child handlers.
func suppressWrapperSignals() func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, wrapperSignals...)
	return func() { signal.Stop(signals) }
}
