// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The root command preserves the original gohmos installer and source pin.
package main

import "github.com/ZxillyFork/go-hmos-installer/internal/toolchain"

func main() { toolchain.LegacyMain() }
