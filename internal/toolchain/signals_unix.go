// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package toolchain

import (
	"os"
	"syscall"
)

var wrapperSignals = []os.Signal{os.Interrupt, syscall.SIGQUIT}
